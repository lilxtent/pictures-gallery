// Package site gives a typed view of the site settings the painter edits:
// her name, texts, contacts and the author photo.
package site

import (
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"

	"github.com/lilxtent/pictures-gallery/internal/images"
	"github.com/lilxtent/pictures-gallery/internal/store"
)

// Setting keys.
const (
	KeyArtistName = "artist_name"
	KeySubtitle   = "subtitle"
	KeyGreeting   = "greeting"
	KeyAboutText  = "about_text"
	KeyAboutPhoto = "about_photo"
	KeyPhone      = "phone"
	KeyEmail      = "email"
	KeyTelegram   = "telegram"
	KeyWhatsApp   = "whatsapp"
	KeyVK         = "vk"
)

// DefaultArtistName is shown until the painter enters her name.
const DefaultArtistName = "Художник"

// Photo describes the processed author photo.
type Photo struct {
	Version int         `json:"version"`
	Width   int         `json:"width"`
	Height  int         `json:"height"`
	Crop    images.Crop `json:"crop"`
}

// Info is everything the page layout needs to know about the site.
type Info struct {
	ArtistName string
	Subtitle   string
	Greeting   string
	AboutText  string
	AboutPhoto *Photo // nil when no photo was uploaded

	Phone    string
	Email    string
	Telegram string
	WhatsApp string
	VK       string
}

// Link is one clickable contact.
type Link struct {
	Label string
	Text  string
	Href  template.URL
}

// Load reads the settings from the store.
func Load(ctx context.Context, st *store.Store) (Info, error) {
	m, err := st.Settings(ctx)
	if err != nil {
		return Info{}, err
	}
	info := Info{
		ArtistName: m[KeyArtistName],
		Subtitle:   m[KeySubtitle],
		Greeting:   m[KeyGreeting],
		AboutText:  m[KeyAboutText],
		Phone:      m[KeyPhone],
		Email:      m[KeyEmail],
		Telegram:   m[KeyTelegram],
		WhatsApp:   m[KeyWhatsApp],
		VK:         m[KeyVK],
	}
	if strings.TrimSpace(info.ArtistName) == "" {
		info.ArtistName = DefaultArtistName
	}
	if raw := m[KeyAboutPhoto]; raw != "" {
		var p Photo
		if err := json.Unmarshal([]byte(raw), &p); err == nil && p.Version > 0 {
			info.AboutPhoto = &p
		}
	}
	return info, nil
}

// Contacts returns links for the filled-in contact fields.
func (i Info) Contacts() []Link {
	var out []Link
	if v := strings.TrimSpace(i.Phone); v != "" {
		out = append(out, Link{"Телефон", v, template.URL("tel:" + keep(v, "+0123456789"))})
	}
	if v := strings.TrimSpace(i.Email); v != "" {
		out = append(out, Link{"Почта", v, template.URL("mailto:" + v)})
	}
	if v := strings.TrimSpace(i.Telegram); v != "" {
		if u := asLink(v); u != "" {
			out = append(out, Link{"Telegram", display(u), template.URL(u)})
		} else {
			name := strings.TrimPrefix(v, "@")
			out = append(out, Link{"Telegram", "@" + name, template.URL("https://t.me/" + url.PathEscape(name))})
		}
	}
	if v := strings.TrimSpace(i.WhatsApp); v != "" {
		digits := keep(v, "0123456789")
		if len(digits) == 11 && digits[0] == '8' {
			digits = "7" + digits[1:]
		}
		if digits != "" {
			out = append(out, Link{"WhatsApp", v, template.URL("https://wa.me/" + digits)})
		}
	}
	if v := strings.TrimSpace(i.VK); v != "" {
		u := asLink(v)
		if u == "" {
			u = "https://vk.com/" + url.PathEscape(v)
		}
		out = append(out, Link{"ВКонтакте", display(u), template.URL(u)})
	}
	return out
}

// keep returns s with only the runes listed in allowed.
func keep(s, allowed string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(allowed, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// asLink returns v as an https/http URL if it looks like a link, else "".
func asLink(v string) string {
	lower := strings.ToLower(v)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		return v
	case strings.Contains(v, "/"):
		return "https://" + v
	}
	return ""
}

// display strips the scheme, "www." and a trailing slash for showing a link.
func display(u string) string {
	for _, p := range []string{"https://", "http://", "www."} {
		if strings.HasPrefix(strings.ToLower(u), p) {
			u = u[len(p):]
		}
	}
	return strings.TrimSuffix(u, "/")
}
