package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coopgo/coopurl"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func main() {

	h, err := coopurl.New(coopurl.WithLogger(logrus.New()))
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	r := mux.NewRouter()

	// HomePage
	r.HandleFunc("/", ServeHome).Methods("GET")
	r.HandleFunc("/", ServeShort(h)).Methods("POST")

	// Redirect
	r.Handle("/r/{key}", h).Methods("GET")

	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:8080",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	fmt.Println("Starting server on : " + srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

func ServeHome(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/layout.html", "templates/home.html"))
	tmpl.ExecuteTemplate(w, "layout", nil)
}

type ShortData struct {
	ShortURL    string
	PrevURL     string
	PrevURLLink string
}

func ServeShort(h *coopurl.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		us := r.Form["u"]
		if len(us) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		u := us[0]

		id, err := h.Post(u)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		ur, err := url.Parse(u)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if ur.Scheme == "" {
			ur.Scheme = "http"
		}

		surl := url.URL{Host: r.Host, Path: "r/" + id}

		data := ShortData{
			ShortURL:    strings.TrimLeft(surl.String(), "/"),
			PrevURL:     u,
			PrevURLLink: ur.String(),
		}

		tmpl := template.Must(template.ParseFiles("templates/layout.html", "templates/short.html"))
		tmpl.ExecuteTemplate(w, "layout", data)
	}
}
