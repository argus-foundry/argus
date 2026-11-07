package api

import "net/http"

func RuntimeRoutesForTest(s *Server) http.Handler { return s.routes() }
