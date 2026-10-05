package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

const maxCategoryNameRunes = 100

const (
	msgCategoryNameRequired = "Укажите название категории"
	msgCategoryNameTooLong  = "Название категории слишком длинное"
	msgCategoryNameTaken    = "Категория с таким названием уже есть"
)

// categoriesView is the data for the categories page. Error belongs to the
// form for ErrorID (0 = the "add" form), and Draft is the text it was sent with.
type categoriesView struct {
	Categories []store.Category
	Error      string
	ErrorID    int64
	Draft      string
}

func (a *Admin) renderCategories(w http.ResponseWriter, r *http.Request, status int, cv categoriesView) {
	categories, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cv.Categories = categories
	a.render(w, r, status, "categories.html", view{Title: "Категории", Nav: "categories", Data: cv})
}

func (a *Admin) categories(w http.ResponseWriter, r *http.Request) {
	a.renderCategories(w, r, http.StatusOK, categoriesView{})
}

// checkCategoryName trims name and returns it with an error message, or ""
// when it is fine. selfID is the category being renamed (0 when adding).
func (a *Admin) checkCategoryName(r *http.Request, name string, selfID int64) (string, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case name == "":
		return name, msgCategoryNameRequired, nil
	case utf8.RuneCountInString(name) > maxCategoryNameRunes:
		return name, msgCategoryNameTooLong, nil
	}
	existing, err := a.st.ListCategories(r.Context())
	if err != nil {
		return name, "", err
	}
	for _, c := range existing {
		if c.ID != selfID && strings.EqualFold(c.Name, name) {
			return name, msgCategoryNameTaken, nil
		}
	}
	return name, "", nil
}

func (a *Admin) createCategory(w http.ResponseWriter, r *http.Request) {
	name, msg, err := a.checkCategoryName(r, r.PostFormValue("name"), 0)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if msg != "" {
		a.renderCategories(w, r, http.StatusUnprocessableEntity, categoriesView{Error: msg, Draft: name})
		return
	}
	c := store.Category{Name: name}
	if err := a.st.CreateCategory(r.Context(), &c); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/categories?msg=saved", http.StatusSeeOther)
}

// categoryFromPath loads the category named by the {id} path segment,
// writing a 404/500 response and returning false when it cannot.
func (a *Admin) categoryFromPath(w http.ResponseWriter, r *http.Request) (store.Category, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return store.Category{}, false
	}
	c, err := a.st.GetCategory(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return store.Category{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.Category{}, false
	}
	return c, true
}

func (a *Admin) renameCategory(w http.ResponseWriter, r *http.Request) {
	c, ok := a.categoryFromPath(w, r)
	if !ok {
		return
	}
	name, msg, err := a.checkCategoryName(r, r.PostFormValue("name"), c.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if msg != "" {
		a.renderCategories(w, r, http.StatusUnprocessableEntity, categoriesView{Error: msg, ErrorID: c.ID, Draft: name})
		return
	}
	if err := a.st.RenameCategory(r.Context(), c.ID, name); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/categories?msg=saved", http.StatusSeeOther)
}

func (a *Admin) reorderCategories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.st.ReorderCategories(r.Context(), body.IDs); err != nil {
		a.log.Error("reorder categories", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Admin) deleteCategoryConfirm(w http.ResponseWriter, r *http.Request) {
	c, ok := a.categoryFromPath(w, r)
	if !ok {
		return
	}
	// Count is not loaded by GetCategory, so take it from the list.
	all, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, other := range all {
		if other.ID == c.ID {
			c.Count = other.Count
		}
	}
	a.render(w, r, http.StatusOK, "category_delete.html", view{Title: "Удалить категорию", Nav: "categories", Data: c})
}

func (a *Admin) deleteCategory(w http.ResponseWriter, r *http.Request) {
	c, ok := a.categoryFromPath(w, r)
	if !ok {
		return
	}
	if err := a.st.DeleteCategory(r.Context(), c.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/categories?msg=category-deleted", http.StatusSeeOther)
}
