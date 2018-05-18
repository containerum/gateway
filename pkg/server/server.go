package server

import (
	"fmt"
	slog "log"
	"net/http"

	"git.containerum.net/ch/api-gateway/pkg/model"
	middle "git.containerum.net/ch/api-gateway/pkg/server/middleware"
	"git.containerum.net/ch/auth/proto"
	"github.com/containerum/cherry/adaptors/cherrylog"
	"github.com/containerum/cherry/adaptors/gonic"
	"github.com/containerum/kube-client/pkg/cherry/api-gateway"
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
	Auth    *authProto.AuthClient
	Metrics *model.Metrics
}

//New return configurated server with all handlers
func New(opt *Options) (serve *Server, err error) {
	var handlers http.Handler
	if handlers, err = createHandler(opt); err != nil {
		return nil, err
	}
	serve = &Server{
		options: opt,
		Server: http.Server{
			Addr:     fmt.Sprintf("0.0.0.0:%v", opt.Config.Port),
			Handler:  handlers,
			ErrorLog: slog.New(log.New().Writer(), "server", 0),
		},
	}
	return
}

//Start return http or https ListenServer
func (s *Server) Start() error {
	if s.options.Config.TLS.Enable {
		return s.Server.ListenAndServeTLS(s.options.Config.TLS.Cert, s.options.Config.TLS.Key)
	}
	return s.ListenAndServe()
}

func registreMiddlewares(router *gin.Engine, opt *Options, limiter *middle.Limiter) {
	router.Use(gonic.Recovery(gatewayErrors.ErrInternal, cherrylog.NewLogrusAdapter(log.WithField("component", "gin_recovery"))))
	router.Use(middle.Logger(opt.Metrics), middle.Cors())
	router.Use(limiter.Limit())
	router.Use(middle.SetHeaderFromQuery(), middle.ClearXHeaders(), middle.SetRequestID())
	router.Use(middle.CheckUserClientHeader(), middle.SetMainUserXHeaders())
}

func createHandler(opt *Options) (http.Handler, error) {
	router := gin.New()
	registreMiddlewares(router, opt, middle.CreateLimiter(opt.Config.Rate.Limit))
	for _, route := range opt.Routes.Routes {
		if route.Active {
			if opt.Config.Auth.Enable {
				router.Handle(route.Method, route.Listen, middle.SetRequestName(route.ID), middle.CheckAuth(route.Roles, opt.Auth), proxyHandler(route))
			} else {
				router.Handle(route.Method, route.Listen, middle.SetRequestName(route.ID), proxyHandler(route))
			}
			route.Entry().Info("Route added")
		}
	}
	return router, nil
}
