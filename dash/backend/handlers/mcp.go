package handlers

import (
	"net/http"

	"github.com/danilrybalkin/apollo-dash/tools"
)

func MCPHandler(w http.ResponseWriter, r *http.Request) {
	tools.MCPHandler(w, r)
}

func InitMCPServers() {
	tools.InitMCPServers()
}
