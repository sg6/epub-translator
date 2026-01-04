package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
)

type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	apiUrl := os.Getenv("GEMINI_API_URL")
	model := os.Getenv("GEMINI_MODEL")

	if apiKey == "" || apiUrl == "" || model == "" {
		log.Fatal("GEMINI_API_KEY, GEMINI_API_URL, and GEMINI_MODEL must be set")
	}

	if len(os.Args) < 2 {
		log.Fatal("Usage: epub-translator <input.epub>")
	}

	inputPath := os.Args[1]
	outputPath := "output.epub"

	err := processEpub(inputPath, outputPath, apiKey, apiUrl, model)
	if err != nil {
		log.Fatalf("Error processing epub: %v", err)
	}

	fmt.Printf("Successfully translated EPUB to %s\n", outputPath)
}

func processEpub(inputPath, outputPath, apiKey, apiUrl, model string) error {
	reader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("could not open input epub: %w", err)
	}
	defer reader.Close()

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("could not create output file: %w", err)
	}
	defer outputFile.Close()

	writer := zip.NewWriter(outputFile)
	defer writer.Close()

	for _, file := range reader.File {
		err := processFile(file, writer, apiKey, apiUrl, model)
		if err != nil {
			return fmt.Errorf("error processing file %s: %w", file.Name, err)
		}
	}

	return nil
}

func processFile(file *zip.File, writer *zip.Writer, apiKey, apiUrl, model string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	w, err := writer.Create(file.Name)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(file.Name))
	if ext == ".xhtml" || ext == ".html" {
		log.Printf("Translating %s...", file.Name)
		return translateHTML(rc, w, apiKey, apiUrl, model)
	}

	_, err = io.Copy(w, rc)
	return err
}

func translateHTML(r io.Reader, w io.Writer, apiKey, apiUrl, model string) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return err
	}

	// Tags to translate
	doc.Find("p, h1, h2, h3, h4, h5, h6, li, span").Each(func(i int, s *goquery.Selection) {
		// Only translate if there's text and it's not just whitespace
		if strings.TrimSpace(s.Text()) == "" {
			return
		}

		// Use innerHTML to keep nested tags like <em> or <strong>
		inner, err := s.Html()
		if err != nil {
			return
		}

		translated := translateNode(inner, apiKey, apiUrl, model)
		s.SetHtml(translated)
	})

	htmlStr, err := doc.Html()
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, htmlStr)
	return err
}

func translateNode(htmlContent, key, url, model string) string {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a professional translator. Translate to German. Keep all HTML tags exactly as they are. Output ONLY the translated content."},
			{"role": "user", "content": htmlContent},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))

	if err != nil {
		return htmlContent
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error calling API: %v", err)
		return htmlContent
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("API returned non-OK status: %d", resp.StatusCode)
		return htmlContent
	}

	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var openAIResp OpenAIResponse

	if err := json.Unmarshal(respBody, &openAIResp); err != nil || len(openAIResp.Choices) == 0 {
		return htmlContent
	}

	return strings.TrimSpace(openAIResp.Choices[0].Message.Content)
}
