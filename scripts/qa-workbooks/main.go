// QA-only workbook fixture builder. Only accepts explicitly named scratch databases.
package main

import (
	"bytes"
	"context"
	"flag"
	"github.com/xuri/excelize/v2"
	"log"
	"os"
	"path/filepath"
	"standard-setting/backend/internal/service"
	"standard-setting/backend/internal/store"
	"strings"
	"time"
)

func main() {
	id := flag.String("project", "", "scratch project ID")
	database := flag.String("database", "standard-setting-qa-admin-20261014", "scratch database")
	output := flag.String("output", "../qa-artifacts/admin-20261014", "output directory")
	flag.Parse()
	if *id == "" || !strings.HasPrefix(*database, "standard-setting-qa-") {
		log.Fatal("explicit QA project and scratch database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.NewMongo(ctx, "mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true", *database)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close(ctx)
	s := service.New(st, nil, "12345*")
	os.MkdirAll(*output, 0755)
	for step := 2; step <= 4; step++ {
		data, err := s.Template(ctx, *id, step)
		if err != nil {
			log.Fatal(err)
		}
		name := []string{"", "", "template-butir.xlsx", "template-nama.xlsx", "template-username.xlsx"}[step]
		if err := os.WriteFile(filepath.Join(*output, name), data, 0644); err != nil {
			log.Fatal(err)
		}
		if step == 2 {
			f, err := excelize.OpenReader(bytes.NewReader(data))
			if err != nil {
				log.Fatal(err)
			}
			f.SetCellValue("Data", "A3", "Mapel Tidak Ada")
			f.SetCellValue("Data", "B3", "UNKNOWN-ID")
			if err := f.SaveAs(filepath.Join(*output, "invalid-butir-row3.xlsx")); err != nil {
				log.Fatal(err)
			}
			f.Close()
		}
		if step == 4 {
			f, err := excelize.OpenReader(bytes.NewReader(data))
			if err != nil {
				log.Fatal(err)
			}
			rows, _ := f.GetRows("Data")
			for n := 1; n < len(rows); n++ {
				cell, _ := excelize.CoordinatesToCellName(4, n+1)
				value, _ := f.GetCellValue("Data", cell)
				if value == "" {
					f.SetCellValue("Data", cell, "qa.custom."+*id+"."+strings.ReplaceAll(cell, "D", ""))
				}
			}
			if err := f.SaveAs(filepath.Join(*output, "valid-custom-username.xlsx")); err != nil {
				log.Fatal(err)
			}
			f.Close()
		}
	}
	log.Print("QA workbook fixtures exported")
}
