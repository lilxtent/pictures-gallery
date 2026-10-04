package admin

import (
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/lilxtent/pictures-gallery/internal/gallery"
	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/site"
)

type aboutView struct {
	Text   string
	Crop   cropField
	Errors gallery.FieldErrors
}

func aboutCrop(info site.Info, errMsg string) cropField {
	cf := cropField{Error: errMsg}
	if p := info.AboutPhoto; p != nil {
		cf.CurrentURL = images.URL(images.AboutDir, p.Version, 600)
		cf.OriginalURL = "/admin/about/original"
		cf.CropJSON = cropJSON(p.Crop)
	}
	return cf
}

func (a *Admin) aboutForm(w http.ResponseWriter, r *http.Request) {
	info, err := site.Load(r.Context(), a.st)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "about.html", view{Title: "Об авторе", Nav: "about",
		Data: aboutView{Text: info.AboutText, Crop: aboutCrop(info, "")}})
}

func (a *Admin) saveAbout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	text := gallery.CleanText(r.PostFormValue("about_text"))
	errs := gallery.FieldErrors{}
	data, err := readPhoto(r)
	if err != nil {
		errs["photo"] = photoError(err)
		if errs["photo"] == "" {
			errs["photo"] = msgPhotoUnreadable
		}
	}
	if len(errs) == 0 {
		var photo *gallery.Photo
		switch {
		case data != nil:
			photo = &gallery.Photo{Original: data, Crop: parseCrop(r)}
		case cropChanged(r):
			photo = &gallery.Photo{Crop: parseCrop(r)}
		}
		if photo != nil {
			if err := a.g.SetAboutPhoto(ctx, *photo); err != nil {
				msg := photoError(err)
				if msg == "" {
					a.serverError(w, r, err)
					return
				}
				errs["photo"] = msg
			}
		}
	}
	if len(errs) == 0 {
		if err := a.st.SetSettings(ctx, map[string]string{site.KeyAboutText: text}); err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/about?msg=saved", http.StatusSeeOther)
		return
	}
	info, err := site.Load(ctx, a.st)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusUnprocessableEntity, "about.html", view{Title: "Об авторе", Nav: "about",
		Data: aboutView{Text: text, Crop: aboutCrop(info, errs["photo"]), Errors: errs}})
}

func (a *Admin) aboutOriginal(w http.ResponseWriter, r *http.Request) {
	a.serveOriginal(w, r, images.AboutDir)
}

var settingsFields = []string{
	site.KeyArtistName, site.KeySubtitle, site.KeyGreeting,
	site.KeyPhone, site.KeyEmail, site.KeyTelegram, site.KeyWhatsApp, site.KeyVK,
}

type settingsView struct {
	Values         map[string]string
	Errors         gallery.FieldErrors
	PasswordErrors gallery.FieldErrors
}

func (a *Admin) renderSettings(w http.ResponseWriter, r *http.Request, status int, sv settingsView) {
	a.render(w, r, status, "settings.html", view{Title: "Настройки", Nav: "settings", Data: sv})
}

func (a *Admin) settingsForm(w http.ResponseWriter, r *http.Request) {
	values, err := a.st.Settings(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderSettings(w, r, http.StatusOK, settingsView{Values: values})
}

func (a *Admin) saveSettings(w http.ResponseWriter, r *http.Request) {
	values := map[string]string{}
	for _, k := range settingsFields {
		values[k] = strings.TrimSpace(r.PostFormValue(k))
	}
	values[site.KeyGreeting] = gallery.CleanText(r.PostFormValue(site.KeyGreeting))
	errs := gallery.FieldErrors{}
	if e := values[site.KeyEmail]; e != "" {
		if addr, err := mail.ParseAddress(e); err != nil || addr.Address != e {
			errs[site.KeyEmail] = "Проверьте адрес почты"
		}
	}
	if len(errs) > 0 {
		a.renderSettings(w, r, http.StatusUnprocessableEntity, settingsView{Values: values, Errors: errs})
		return
	}
	if err := a.st.SetSettings(r.Context(), values); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/settings?msg=saved", http.StatusSeeOther)
}

func (a *Admin) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ok, err := checkPassword(ctx, a.st, r.PostFormValue("current"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	newPw, repeat := r.PostFormValue("new"), r.PostFormValue("repeat")
	errs := gallery.FieldErrors{}
	switch {
	case !ok:
		errs["current"] = "Неверный текущий пароль"
	case utf8.RuneCountInString(newPw) < 8:
		errs["new"] = "Новый пароль должен быть не короче 8 символов"
	case len(newPw) > 72: // bcrypt rejects longer passwords
		errs["new"] = "Новый пароль слишком длинный (не больше 72 латинских или 36 русских букв)"
	case newPw != repeat:
		errs["repeat"] = "Пароли не совпадают"
	}
	if len(errs) > 0 {
		values, err := a.st.Settings(ctx)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		a.renderSettings(w, r, http.StatusUnprocessableEntity, settingsView{Values: values, PasswordErrors: errs})
		return
	}
	if err := setPassword(ctx, a.st, newPw); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/settings?msg=password", http.StatusSeeOther)
}
