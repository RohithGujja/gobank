package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

type APIServer struct {
	listenAddr string
	storage    Storage
}

func NewAPIServer(addr string, s Storage) *APIServer {
	return &APIServer{
		listenAddr: addr,
		storage:    s,
	}
}

func (s *APIServer) Run() {
	router := mux.NewRouter()

	router.HandleFunc("/account", makeHTTPHandlerFunc(s.handleAccount))
	router.HandleFunc("/account/{id}", withJWTAuth(makeHTTPHandlerFunc(s.handleAccountByID), s.storage))
	router.HandleFunc("/transfer", makeHTTPHandlerFunc(s.handleTransfer))

	log.Println("API server is running on port:", s.listenAddr)

	err := http.ListenAndServe(s.listenAddr, router)
	if err != nil {
		panic(err)
	}
}

func (s *APIServer) handleAccount(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		return s.handleGetAllAccounts(w, r)
	case http.MethodPost:
		return s.handleCreateAccount(w, r)
	default:
		return fmt.Errorf("method not allowed, %s", r.Method)
	}
}

func (s *APIServer) handleGetAllAccounts(w http.ResponseWriter, r *http.Request) error {
	accounts, err := s.storage.GetAllAccounts()
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, accounts)
}

func (s *APIServer) handleAccountByID(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		{
			id, err := getId(r)
			if err != nil {
				return err
			}

			account, err := s.storage.GetAccountByID(id)
			if err != nil {
				return fmt.Errorf("error occured while fetching account details: %w", err)
			}

			return WriteJSON(w, http.StatusOK, account)
		}
	case http.MethodDelete:
		return s.handleDeleteAccount(w, r)
	default:
		return fmt.Errorf("method not allowed, %s", r.Method)
	}
}

func (s *APIServer) handleCreateAccount(w http.ResponseWriter, r *http.Request) error {
	req := new(CreateAccountRequest)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		return err
	}

	account := NewAccount(req.FirstName, req.LastName)
	err := s.storage.CreateAccount(account)
	if err != nil {
		return err
	}

	tokenString, err := createJWT(account)
	if err != nil {
		return err
	}

	fmt.Println("JWT token:", tokenString)

	return WriteJSON(w, http.StatusOK, account)
}

func (s *APIServer) handleDeleteAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := getId(r)
	if err != nil {
		return err
	}

	_, err = s.storage.GetAccountByID(id)
	if err != nil {
		return fmt.Errorf("error occured while deleting account details: %w", err)
	}

	err = s.storage.DeleteAccount(id)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, map[string]int{"account deleted successfully with id": id})
}

func (s *APIServer) handleTransfer(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return fmt.Errorf("method not allowed, %s", r.Method)
	}
	transferRequest := new(TransferRequest)
	err := json.NewDecoder(r.Body).Decode(transferRequest)
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, transferRequest)
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func permissionDenied(w http.ResponseWriter) {
	WriteJSON(w, http.StatusOK, ApiError{Error: "permission denied"})
}

func withJWTAuth(handlerFunc http.HandlerFunc, s Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("calling JWT middlewares")

		token, err := validateJWT(r.Header.Get("x-jwt-token"))
		if err != nil || !token.Valid {
			permissionDenied(w)
			return
		}

		id, err := getId(r)
		if err != nil {
			permissionDenied(w)
			return
		}

		account, err := s.GetAccountByID(id)
		if err != nil {
			permissionDenied(w)
			return
		}

		claims := token.Claims.(jwt.MapClaims)
		if account.Number != int64(claims["accountNumber"].(float64)) {
			permissionDenied(w)
			return
		}
		handlerFunc(w, r)
	}
}

func createJWT(account *Account) (string, error) {

	secret := os.Getenv("JWT_TEST_SECRET")

	// Create the Claims and token
	claims := &jwt.MapClaims{
		"expiresAt":     15000,
		"accountNumber": account.Number,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(secret))
}

func validateJWT(tokenString string) (*jwt.Token, error) {
	secret := os.Getenv("JWT_TEST_SECRET")

	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(secret), nil
	})
}

type apiFunc func(http.ResponseWriter, *http.Request) error

type ApiError struct {
	Error string `json:"error"`
}

func makeHTTPHandlerFunc(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			WriteJSON(w, http.StatusBadRequest, ApiError{Error: err.Error()})
		}
	}
}

func getId(r *http.Request) (int, error) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return id, fmt.Errorf("invalid id provided: '%s'", idStr)
	}
	return id, nil
}
