package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/silentsharer/grace"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		d := r.URL.Query().Get("duration")
		if len(d) != 0 {
			t, _ := time.ParseDuration(d)
			time.Sleep(t)
		}
		fmt.Fprintln(w, "hello world")
	})

	log.Fatalln(grace.ListenAndServe(":8080", nil))
}
