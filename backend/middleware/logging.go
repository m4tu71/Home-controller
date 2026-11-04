package middleware

import (
	"log"
	"net/http"
)

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realIP := r.Header.Get("X-Real-IP")
		forwardedFor := r.Header.Get("X-Forwarded-For")

		log.Printf(
			"method=%s path=%s remote=%s real_ip=%s forwarded_for=%s user_agent=%q referer=%q",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			realIP,
			forwardedFor,
			r.UserAgent(),
			r.Referer(),
		)

		next.ServeHTTP(w, r)
	})
}
