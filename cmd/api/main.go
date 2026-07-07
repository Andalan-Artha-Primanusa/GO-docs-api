package main

import (
	"flag"
	"log"
	"net/http"

	"rbac-request-engine/internal/app"
	"rbac-request-engine/internal/config"
	"rbac-request-engine/internal/database"
)

func main() {
	runMigrate := flag.Bool("migrate", false, "run database migrations")
	runSeed := flag.Bool("seed", false, "seed default data")
	flag.Parse()

	cfg := config.Load()
	if *runMigrate {
		if err := database.EnsureDatabase(cfg.MySQLDSN); err != nil {
			log.Fatal(err)
		}
	}
	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if *runMigrate {
		if err := database.Migrate(db); err != nil {
			log.Fatal(err)
		}
		log.Println("migrations completed")
	}

	if *runSeed {
		if err := database.Seed(db); err != nil {
			log.Fatal(err)
		}
		log.Println("seed completed")
	}
	if *runMigrate || *runSeed {
		return
	}

	srv := app.New(db, cfg)
	log.Printf("upload storage=%s upload_dir=%s ftp_host=%s ftp_dir=%s", cfg.UploadStorage, cfg.UploadDir, cfg.FTPHost, cfg.FTPDir)
	log.Printf("api listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, srv.Routes()))
}
