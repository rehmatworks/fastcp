package main

import (
	"fmt"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

func main() {
	cfg, _ := config.Load("")
	cfg.JWTSecret = cfg.JWTSecret
	config.Update(cfg)
	user := &models.User{ID: "0", Username: "root", Role: "admin"}
	token, err := auth.GenerateToken(user)
	if err != nil {
		panic(err)
	}
	fmt.Println(token)
}
