package server

import (
	"fmt"
	"net/http"
)

const (
	DefaultPort    = 8080
	DefaultTimeout = 30
)

var globalConfig = Config{Port: DefaultPort}

type Config struct {
	Port    int
	Timeout int
}

type Handler struct {
	config *Config
}

func NewHandler(config *Config) *Handler {
	return &Handler{config: config}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World!")
}
