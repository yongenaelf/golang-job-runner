package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func extractZip(zipFile, dest string) error {
	r, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func findCsprojFile(root string, isTest bool) (string, error) {
	var csprojPath string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		var suffix string
		if isTest {
			suffix = ".Tests.csproj"
		} else {
			suffix = ".csproj"
		}

		if strings.HasSuffix(info.Name(), suffix) {
			csprojPath = path
			return filepath.SkipDir // Stop walking once we found a .csproj
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if csprojPath == "" {
		return "", fmt.Errorf("no .csproj file found")
	}
	return csprojPath, nil
}

func runDotNetBuild(csprojPath string) (string, string, error) {
	projectDir := filepath.Dir(csprojPath)
	cmd := exec.Command("dotnet", "build")
	cmd.Dir = projectDir

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", stdout.String(), err
	}

	// Locate the build output directory
	outputDir := filepath.Join(projectDir, "bin", "Debug")
	return outputDir, stdout.String(), nil
}

func runDotNetTest(csprojPath string) (string, error) {
	projectDir := filepath.Dir(csprojPath)
	cmd := exec.Command("dotnet", "test")
	cmd.Dir = projectDir

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	var err = cmd.Run()

	return stdout.String(), err
}

func encodeDllToBase64(outputDir string) (string, error) {
	var dllFile string

	// Expected to find our .dll file in the output directory
	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(info.Name(), ".dll") {
			dllFile = path
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if dllFile == "" {
		return "", fmt.Errorf("no .dll file found in build output")
	}

	dllContent, err := os.ReadFile(dllFile)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(dllContent), nil
}

func deleteFile(path string) {
	err := os.RemoveAll(path)
	if err != nil {
		fmt.Printf("Failed to delete %s: %v\n", path, err)
	}
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 10 << 20 // 10 MB
	r.ParseMultipartForm(maxUploadSize)
	params := r.URL.Query()
	command := params.Get("command")

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	guid := uuid.New()
	fileName := guid.String() + ".zip"

	log.Printf("Request time: %v\n", time.Now())
	log.Printf("Starting job for: %s\n", fileName)
	measureStart := time.Now()

	uploadPath := "./uploads/"
	filePath := uploadPath + fileName

	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Unable to create the file for writing", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	extractDir := "./extracted/" + guid.String()
	if err := os.MkdirAll(extractDir, os.ModePerm); err != nil {
		http.Error(w, "Failed to create extraction directory", http.StatusInternalServerError)
		return
	}

	defer func() {
		log.Printf("Job for %s took %v\n", fileName, time.Since(measureStart))
		// Ensure cleanup happens regardless of success or failure
		deleteFile(filePath)
		deleteFile(extractDir)
	}()

	if err := extractZip(filePath, extractDir); err != nil {
		http.Error(w, "Failed to extract zip file", http.StatusInternalServerError)
		return
	}

	csprojPath, err := findCsprojFile(extractDir, command == "test")
	if err != nil {
		http.Error(w, "Failed to find .csproj file", http.StatusInternalServerError)
		return
	}

	if command == "test" {
		stdout, err := runDotNetTest(csprojPath)
		if err != nil {
			http.Error(w, "Test failed: "+stdout, http.StatusInternalServerError)
			return
		}
	}

	outputDir, stdout, err := runDotNetBuild(csprojPath)
	if err != nil {
		http.Error(w, "Build failed: "+stdout, http.StatusInternalServerError)
		return
	}

	dllBase64, err := encodeDllToBase64(outputDir)
	if err != nil {
		http.Error(w, "Failed to encode .dll file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Build output:\n" + stdout + "\n\nEncoded DLL:\n" + dllBase64))
}

func main() {
	err := os.MkdirAll("./uploads", os.ModePerm)
	if err != nil {
		fmt.Println("Failed to create upload directory:", err)
		return
	}

	http.HandleFunc("/upload", handleUpload)
	http.Handle("/", http.FileServer(http.Dir("./")))

	fmt.Println("Server listening on port 8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Failed to start server:", err)
	}
}
