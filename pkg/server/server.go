package server

import (
	"context"
	"fmt"
	slog "log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"git.containerum.net/ch/api-gateway/pkg/gatewayErrors"
	"git.containerum.net/ch/api-gateway/pkg/model"
	middle "git.containerum.net/ch/api-gateway/pkg/server/middleware"
	"git.containerum.net/ch/auth/proto"
	"github.com/containerum/cherry/adaptors/cherrylog"
	"github.com/containerum/cherry/adaptors/gonic"
	"github.com/gin-gonic/gin"

	log "github.com/sirupsen/logrus"
)

//Server keeps HTTP sever and it configs
type Server struct {
	http.Server
	options *Options
}

//Options keeps params for gateway server runtime
type Options struct {
	Routes  *model.Routes
	Config  *model.Config
	Auth    authProto.AuthClient
	Metrics *model.Metrics

	ServiceHostPrefix string
	Version           string // will be in status
}

//New return configurated server with all handlers
func New(opt *Options) (server *Server, err error) {
	server = &Server{
		options: opt,
		Server: http.Server{
			Addr:     fmt.Sprintf("0.0.0.0:%v", opt.Config.Port),
			ErrorLog: slog.New(log.New().Writer(), "server", 0),
		},
	}

	server.Handler, err = server.createHandler()
	return
}

func (s *Server) addPrefixToBackendURL(backend *url.URL) *url.URL {
	newUrl := *backend
	if s.options.ServiceHostPrefix != "" {
		newUrl.Host = s.options.ServiceHostPrefix + "-" + newUrl.Host
	}
	return &newUrl
}

//Start return http or https ListenServer
func (s *Server) Start() error {
	errCh := make(chan error)
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt, os.Kill)
	go func() {
		if s.options.Config.TLS.Enable {
			errCh <- s.Server.ListenAndServeTLS(s.options.Config.TLS.Cert, s.options.Config.TLS.Key)
		}
		errCh <- s.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-quit:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.Server.Shutdown(ctx)
	}
}

func (s *Server) registerMiddlewares(router *gin.Engine) {
	router.Use(
		gonic.Recovery(gatewayErrors.ErrInternal, cherrylog.NewLogrusAdapter(log.WithField("component", "gin_recovery"))),
		middle.Logger(s.options.Metrics),
		middle.Cors(),
		middle.CreateLimiter(s.options.Config.Rate.Limit).Limit(),
		middle.SetHeaderFromQuery(),
		middle.ClearXHeaders(),
		middle.SetRequestID(),
		middle.CheckUserClientHeader(),
		middle.SetMainUserXHeaders(),
	)
}

func (s *Server) createHandler() (http.Handler, error) {
	router := gin.New()
	s.registerMiddlewares(router)
	for _, route := range s.options.Routes.Routes {
		if route.Active {
			handlers := []gin.HandlerFunc{middle.SetRequestName(route.ID)}
			if s.options.Config.Auth.Enable {
				handlers = append(handlers, middle.CheckAuth(route.Roles, s.options.Auth))
			}
			handlers = append(handlers, s.proxyHandler(route))

			router.Handle(route.Method, route.Listen, handlers...)
			route.Entry().Info("Route added")
		}
	}
	router.GET("/status", s.healthCheckHandler)
	router.NoMethod(func(ctx *gin.Context) {
		gonic.Gonic(gatewayErrors.
			ErrMethodNotAllowed().
			AddDetailF("method %s not registered for route %s", ctx.Request.Method, ctx.Request.URL.Path),
			ctx)
	})
	router.NoRoute(func(ctx *gin.Context) {
		gonic.Gonic(gatewayErrors.
			ErrRouteNotFound().
			AddDetailF("route %s not registered", ctx.Request.URL.Path),
			ctx)
	})
	return router, nil
}
