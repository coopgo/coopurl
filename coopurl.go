// coopurl is an url shortener library using badger.
package coopurl

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
)

const (
	DefaultDbPath = "/tmp/badger"
	DefaultLength = 8
)

// Handler is the handler for our library.
// It should be created with the New() function.
type Handler struct {
	db     *badger.DB
	mu     sync.Mutex
	path   string // will only affect the database if it's set before the database is initialized.
	logger Logger

	TTL    time.Duration
	Length int
	Scheme string
}

// New creates a new Handler.
// It implements the option pattern to change the default value of our handler.
func New(opts ...Options) (*Handler, error) {
	var h Handler
	h.logger = NilLogger{}

	for _, opt := range opts {
		opt(&h)
	}

	if err := h.open(); err != nil {
		return nil, err
	}

	return &h, nil
}

type Options func(*Handler)

func WithDbPath(path string) Options {
	return func(h *Handler) {
		h.path = path
	}
}

func WithDefaultTTL(ttl time.Duration) Options {
	return func(h *Handler) {
		h.TTL = ttl
	}
}

func WithDefaultLength(length int) Options {
	return func(h *Handler) {
		h.Length = length
	}
}

func WithLogger(logger badger.Logger) Options {
	return func(h *Handler) {
		h.logger = logger
	}
}

func (h *Handler) init() error {
	if h.logger == nil {
		h.logger = NilLogger{}
	}

	if h.db == nil {
		return h.open()
	}
	return nil
}

func (h *Handler) getPath() string {
	if h.path == "" {
		return DefaultDbPath
	}
	return h.path
}

func (h *Handler) getLength(r req) int {
	if r.length > 0 {
		return r.length
	}
	if h.Length > 0 {
		return h.Length
	}
	return DefaultLength
}

func (h *Handler) open() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var err error
	opt := badger.DefaultOptions(h.getPath())
	opt = opt.WithLogger(h.logger)
	h.db, err = badger.Open(opt)
	return err
}

// Close stops the database connection.
func (h *Handler) Close() {
	h.logger.Infof("Closing handler")
	h.db.Close()
}

// ServeHTTP is an http.HandleFunc that will redirect the client to the url linked to the id given in the request url.
// This id is the last part of request url path. eg: "domain.com/r/{id}"
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.init(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}

	// Maybe check only for get methods
	_, id := path.Split(r.URL.Path)
	u, err := h.Get(id)
	if err != nil {
		// TODO: differentiate between not found and other errors
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	h.logger.Infof("Redirect from %s to %s", id, u)

	if err := redirect(w, r, u); err != nil {
		h.logger.Errorf("Couldn't redirect to %s", u)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func redirect(w http.ResponseWriter, r *http.Request, s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	return nil
}

// Get search the store for the url linked to the given id.
func (h *Handler) Get(id string) (string, error) {
	if err := h.init(); err != nil {
		return "", err // Maybe wrap err with custom error
	}
	return h.get(id)
}

func (h *Handler) get(id string) (string, error) {

	var url string
	err := h.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(id))
		if err != nil {
			return err
		}

		b, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		url = string(b)
		return nil
	})
	if err != nil {
		return "", err
	}

	h.logger.Infof("Get entry: %s - %s", id, url)

	return url, nil
}

// Post will take a url, store it and return an id linked to it.
func (h *Handler) Post(url string, opts ...ReqOptions) (string, error) {
	if err := h.init(); err != nil {
		return "", err // Maybe wrap err with custom error
	}
	return h.post(url)
}

func (h *Handler) post(s string, opts ...ReqOptions) (string, error) {

	// Check that the given url is valid
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}

	// We set the scheme to http if it's missing to redirect to google.fr and not domain/r/google.fr
	if u.Scheme == "" {
		h.logger.Infof("Setting missing scheme to http: %s", u.String())
		u.Scheme = "http"
	}

	r := req{}
	for _, opt := range opts {
		opt(&r)
	}

	// Generate Id
	id := generateId(u.String(), h.getLength(r))
	ttl := r.ttl
	if ttl == 0 {
		ttl = h.TTL
	}

	// Put in db
	err = h.db.Update(func(txn *badger.Txn) error {
		if ttl != 0 {
			e := badger.NewEntry([]byte(id), []byte(u.String())).WithTTL(ttl)
			return txn.SetEntry(e)
		}
		return txn.Set([]byte(id), []byte(u.String()))
	})
	if err != nil {
		return "", err
	}

	if ttl != 0 {
		h.logger.Infof("New entry: %s - %s (ttl: %s)", id, u.String(), ttl)
	} else {
		h.logger.Infof("New entry: %s - %s", id, u.String())
	}

	return id, err
}

type ReqOptions func(*req)

func WithTTL(ttl time.Duration) ReqOptions {
	return func(r *req) {
		r.ttl = ttl
	}
}

func WithLength(length int) ReqOptions {
	return func(r *req) {
		r.length = length
	}
}

type req struct {
	ttl    time.Duration
	length int
}

// generateId generates an id from url of size n
func generateId(url string, n int) string {
	s := fmt.Sprintf("%s-%s", url, time.Now())
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
	if n >= len(sha) {
		return sha
	}
	return sha[:n]
}

type Logger badger.Logger

// NilLogger is a logger that doesn't log
type NilLogger struct{}

var _ Logger = (*NilLogger)(nil)

func (NilLogger) Errorf(string, ...interface{})   {}
func (NilLogger) Warningf(string, ...interface{}) {}
func (NilLogger) Infof(string, ...interface{})    {}
func (NilLogger) Debugf(string, ...interface{})   {}
