package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type envelope map[string]interface{}

// send JSON error response
func (app *application) errorResponse(w http.ResponseWriter, status int, message interface{}) {
	env := envelope{"error": message}
	if err := app.writeJSON(w, status, env, nil); err != nil {
		app.logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// write JSON with optional headers
func (app *application) writeJSON(w http.ResponseWriter, status int, body envelope, headers http.Header) error {
	js, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for k, v := range headers {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(js, '\n'))

	return nil
}

// parse and validate a single JSON object
func (app *application) readJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	const maxBytes = 104856
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var (
			syntaxErr  *json.SyntaxError
			typeErr    *json.UnmarshalTypeError
			invalidErr *json.InvalidUnmarshalError
		)

		switch {
		case errors.As(err, &syntaxErr):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxErr.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", typeErr.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", typeErr.Offset)
		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")
		case errors.As(err, &invalidErr):
			panic(err)
		default:
			return err
		}
	}

	// ensure a single JSON value
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("body must only contain a single JSON value")
	}

	return nil
}
