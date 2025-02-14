package ddhttp

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/ascarter/requestid"
	"github.com/felixge/httpsnoop"
	"github.com/sirupsen/logrus"
	"github.com/unionj-cloud/go-doudou/stringutils"
	"github.com/unionj-cloud/go-doudou/svc/config"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"runtime/debug"
	"strings"
	"time"
)

// Metrics logs some metrics for http request
func Metrics(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := httpsnoop.CaptureMetrics(inner, w, r)
		logrus.Printf(
			"%s\t%s\t%s\t%d\t%d\t%s\n",
			r.RemoteAddr,
			r.Method,
			r.URL,
			m.Code,
			m.Written,
			m.Duration,
		)
	})
}

// Logger logs http request body and response body for debugging
func Logger(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RequestURI(), "/go-doudou/") {
			inner.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rid, _ := requestid.FromContext(r.Context())
		x, err := httputil.DumpRequest(r, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rec := httptest.NewRecorder()
		inner.ServeHTTP(rec, r)

		rawReq := string(x)
		if len(r.Header["Content-Type"]) > 0 && strings.Contains(r.Header["Content-Type"][0], "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rawReq = r.Form.Encode()
		}

		hlog := HttpLog{
			ClientIp:          r.RemoteAddr,
			HttpMethod:        r.Method,
			Uri:               r.URL.RequestURI(),
			Proto:             r.Proto,
			Host:              r.Host,
			ReqContentLength:  r.ContentLength,
			ReqHeader:         r.Header,
			RequestId:         rid,
			RawReq:            rawReq,
			RespBody:          rec.Body.String(),
			StatusCode:        rec.Result().StatusCode,
			RespHeader:        rec.Result().Header,
			RespContentLength: rec.Body.Len(),
			ElapsedTime:       time.Since(start).String(),
			Elapsed:           time.Since(start).Milliseconds(),
		}
		log, _ := json.MarshalIndent(hlog, "", "    ")
		logrus.Debugln(string(log))

		header := rec.Result().Header
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Result().StatusCode)
		rec.Body.WriteTo(w)
	})
}

// Rest set Content-Type to application/json
func Rest(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stringutils.IsEmpty(w.Header().Get("Content-Type")) {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		}
		inner.ServeHTTP(w, r)
	})
}

// BasicAuth adds http basic auth validation
func BasicAuth(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := config.GddManageUser.Load()
		password := config.GddManagePass.Load()
		if stringutils.IsNotEmpty(username) || stringutils.IsNotEmpty(password) {
			user, pass, ok := r.BasicAuth()

			if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Provide user name and password"`)
				w.WriteHeader(401)
				w.Write([]byte("Unauthorised.\n"))
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
}

// Recover handles panic from processing incoming http request
func Recover(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logrus.Errorf("panic: %+v\n\nstacktrace from panic: %s\n", err, string(debug.Stack()))
				http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError)
			}
		}()
		inner.ServeHTTP(w, r)
	})
}
