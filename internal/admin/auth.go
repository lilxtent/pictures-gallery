package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lilxtent/pictures-gallery/internal/store"
)

const (
	cookieName  = "gallery_session"
	sessionTTL  = 30 * 24 * time.Hour
	passwordKey = "password_hash"
)

type ctxKey struct{}

// csrfToken returns the CSRF token of the logged-in request, or "".
func csrfToken(r *http.Request) string {
	v, _ := r.Context().Value(ctxKey{}).(string)
	return v
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// csrfFor derives the per-session CSRF token from the raw session token.
func csrfFor(raw string) string {
	sum := sha256.Sum256([]byte("csrf:" + raw))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EnsurePassword stores the initial admin password if none is stored yet.
func EnsurePassword(ctx context.Context, st *store.Store, initial string) error {
	hash, err := st.Setting(ctx, passwordKey)
	if err != nil {
		return err
	}
	if hash != "" {
		return nil
	}
	if initial == "" {
		return errors.New("admin: ADMIN_PASSWORD must be set on first start")
	}
	return setPassword(ctx, st, initial)
}

func setPassword(ctx context.Context, st *store.Store, pw string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return st.SetSettings(ctx, map[string]string{passwordKey: string(hash)})
}

func checkPassword(ctx context.Context, st *store.Store, pw string) (bool, error) {
	hash, err := st.Setting(ctx, passwordKey)
	if err != nil {
		return false, err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil, nil
}

// session returns the raw session token if the request has a valid session.
func (a *Admin) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	ok, err := a.st.SessionValid(r.Context(), hashToken(c.Value), a.now())
	if err != nil {
		a.log.Error("check session", "err", err)
		return "", false
	}
	return c.Value, ok
}

// requireAuth lets only logged-in users through. For POST requests it also
// limits the body size, parses the form and checks the CSRF token.
func (a *Admin) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		raw, ok := a.session(r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		csrf := csrfFor(raw)
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
			got := r.Header.Get("X-CSRF-Token")
			if got == "" {
				if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
					var tooBig *http.MaxBytesError
					if errors.As(err, &tooBig) {
						a.renderMessage(w, r, http.StatusRequestEntityTooLarge, "Фото слишком большое",
							"Максимальный размер фото — 30 МБ. Вернитесь назад и выберите другое фото.")
						return
					}
					a.renderMessage(w, r, http.StatusBadRequest, "Ошибка формы", "Не удалось прочитать форму. Вернитесь назад и попробуйте ещё раз.")
					return
				}
				got = r.PostFormValue("csrf")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(csrf)) != 1 {
				a.renderMessage(w, r, http.StatusForbidden, "Страница устарела",
					"Страница устарела. Вернитесь назад, обновите её и попробуйте снова.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, csrf)))
	}
}

type loginData struct {
	Error string
}

func (a *Admin) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.session(r); ok {
		http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "login.html", view{Title: "Вход", Data: loginData{}})
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	ctx := r.Context()
	ip := clientIP(r, a.trustProxy)
	now := a.now()
	if !a.limiter.Allow(ip, now) {
		a.render(w, r, http.StatusTooManyRequests, "login.html",
			view{Title: "Вход", Data: loginData{"Слишком много попыток. Попробуйте через 15 минут."}})
		return
	}
	ok, err := checkPassword(ctx, a.st, r.PostFormValue("password"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !ok {
		a.limiter.Fail(ip, now)
		a.render(w, r, http.StatusUnauthorized, "login.html", view{Title: "Вход", Data: loginData{"Неверный пароль"}})
		return
	}
	a.limiter.Reset(ip)
	raw, err := newToken()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.st.DeleteExpiredSessions(ctx, now); err != nil {
		a.log.Error("delete expired sessions", "err", err)
	}
	if err := a.st.CreateSession(ctx, hashToken(raw), now.Add(sessionTTL)); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: raw, Path: "/admin",
		Expires: now.Add(sessionTTL), MaxAge: int(sessionTTL / time.Second),
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/paintings", http.StatusSeeOther)
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := a.st.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			a.log.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/admin", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// clientIP returns the visitor's IP. Behind Caddy (trustProxy) it is the
// last X-Forwarded-For entry, which Caddy sets itself.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter counts failed logins per IP in a sliding window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, fails: map[string][]time.Time{}}
}

func (l *limiter) recent(ip string, now time.Time) []time.Time {
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, ip)
		return nil
	}
	l.fails[ip] = kept
	return kept
}

// Allow reports whether another login attempt from ip is permitted.
func (l *limiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, now)) < l.max
}

// Fail records a failed attempt.
func (l *limiter) Fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[ip] = append(l.recent(ip, now), now)
}

// Reset forgets failures after a successful login.
func (l *limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
