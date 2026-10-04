package admin

import (
	"encoding/json"
	"net/http"
)

func (a *Admin) list(w http.ResponseWriter, r *http.Request) {
	paintings, err := a.st.ListPaintings(r.Context(), false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "paintings.html", view{Title: "Картины", Nav: "paintings", Data: paintings})
}

func (a *Admin) reorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.st.ReorderPaintings(r.Context(), body.IDs); err != nil {
		a.log.Error("reorder", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
