package main

import (
	"embed"
	"log"
	"os"

	"github.com/amelia-fresse/head-relative-topology/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	initialRepo := ""
	if len(os.Args) > 1 {
		initialRepo = os.Args[1]
	} else if wd, err := os.Getwd(); err == nil {
		initialRepo = wd
	}

	svc := app.New(initialRepo)

	a := application.New(application.Options{
		Name:        "hrt",
		Description: "Head, Refs & Tips — where every branch sits relative to origin/main",
		Services:    []application.Service{application.NewService(svc)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	a.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "hrt",
		Width:            420,
		Height:           900,
		MinWidth:         320,
		MinHeight:        400,
		BackgroundColour: application.NewRGB(15, 17, 21),
		URL:              "/",
	})

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
