package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"server/internal/store/pgstore"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

type apiHandler struct {
	q          *pgstore.Queries
	r          *chi.Mux
	upgrader   websocket.Upgrader
	subscribes map[string]map[*websocket.Conn]context.CancelFunc
	mu         *sync.Mutex
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

func NewHandler(q *pgstore.Queries) http.Handler {
	a := apiHandler{
		q: q,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		subscribes: make(map[string]map[*websocket.Conn]context.CancelFunc),
		mu:         &sync.Mutex{},
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/subscribe/{room_id}", a.handleSubscribe)

	r.Route("/api", func(r chi.Router) {
		r.Route("/rooms", func(r chi.Router) {
			r.Post("/", a.handleCreateRoom)
			r.Get("/", a.handleGetRooms)

			r.Route("/{room_id}/messages", func(r chi.Router) {
				r.Get("/", a.handleGetRoomMessages)
				r.Post("/", a.handleCreateRoomMessage)

				r.Route("/{message_id}", func(r chi.Router) {
					r.Get("/", a.handleGetRoomMessage)
					r.Patch("/react", a.handleReactToMessage)
					r.Patch("/answered", a.handleMarkMessageAsAnswered)
					r.Delete("/react", a.handleRemoveReactFromMessage)
				})
			})
		})
	})

	a.r = r

	return a
}

func (h apiHandler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	rawRoomId := chi.URLParam(r, "room_id")

	roomId, err := uuid.Parse(rawRoomId)

	if err != nil {
		http.Error(w, "Invalid room id", http.StatusBadRequest)

		return
	}

	_, err = h.q.GetRoom(r.Context(), roomId)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Room not found", http.StatusBadRequest)

			return
		}

		http.Error(w, "Something went wrong", http.StatusInternalServerError)

		return
	}

	c, err := h.upgrader.Upgrade(w, r, nil)

	if err != nil {
		slog.Warn("Failed to upgrade connection.", "error", err)

		http.Error(w, "Failed to upgrade to WS connection", http.StatusBadRequest)

		return
	}

	defer c.Close()

	h.mu.Lock()

	ctx, cancel := context.WithCancel(r.Context())

	if _, ok := h.subscribes[rawRoomId]; !ok {
		h.subscribes[rawRoomId] = make(map[*websocket.Conn]context.CancelFunc, 0)
	}

	slog.Info("new client connected", "room_id", rawRoomId, "client_ip", r.RemoteAddr)

	h.subscribes[rawRoomId][c] = cancel

	h.mu.Unlock()

	<-ctx.Done()

	h.mu.Lock()

	delete(h.subscribes[rawRoomId], c)

	h.mu.Unlock()
}

func (h apiHandler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	type _body struct {
		Theme string `json:"theme"`
	}
	var body _body

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)

		return
	}

	roomId, err := h.q.InsertRoom(r.Context(), body.Theme)

	if err != nil {
		slog.Error("Failed to insert room", "error", err)

		http.Error(w, "Something went wrong", http.StatusInternalServerError)

		return
	}

	type response struct {
		ID string `json"id"`
	}

	data, _ := json.Marshal(response{ID: roomId.String()})

	w.Header().Set("content-type", "application/json")

	_, _ = w.Write(data)
}

func (h apiHandler) handleGetRooms(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleCreateRoomMessage(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleGetRoomMessage(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleGetRoomMessages(w http.ResponseWriter, r *http.Request) {}

func (h apiHandler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {}
