package main

import (
	"gosmpp-test"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	messageSender, err := gosmpp_test.NewMessageSender()
	if err != nil {
		log.Fatal(err)
	}
	defer messageSender.Close()

	router := chi.NewRouter()
	router.Use(
		middleware.RequestID,
		middleware.RealIP,
		middleware.Logger,
		middleware.Recoverer,
	)

	router.Post("/", func(w http.ResponseWriter, r *http.Request) {
		err := messageSender.SendMessage(r.FormValue("to"), r.FormValue("message"))
		if err != nil {
			log.Println("SendMessage err:", err)
		}

	})

	log.Println("Serving HTTP on :10000")
	log.Fatal(http.ListenAndServe(":10000", router))
}
