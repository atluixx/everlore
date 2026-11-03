package handler

import (
	everlore "everlore/app"
	"net/http"
	"sync"
)

var once sync.Once
var app http.Handler

func Handler_api_index_go(w http.ResponseWriter, r *http.Request) {
	once.Do(func() { app = everlore.NewHandler() })
	app.ServeHTTP(w, r)
}
