package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

const (
	dirName = "."
	apiURL  = "http://localhost:8000/api/v1/buckets/interevm/files"
)

func main() {
	files, err := os.ReadDir(dirName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, fd := range files {
		if fd.IsDir() {
			continue
		}

		path := filepath.Join(dirName, fd.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		file, _ := writer.CreateFormFile("file", fd.Name())
		file.Write(data)

		err = writer.Close()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		resp, err := http.Post(apiURL, writer.FormDataContentType(), &body)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		resp.Body.Close()
		fmt.Println("uploaded:", fd.Name(), resp.Status)
	}
}
