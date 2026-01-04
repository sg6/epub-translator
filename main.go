package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	//"github.com/PuerkitoBio/goquery"
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
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	apiUrl := os.Getenv("GEMINI_API_URL")
	model := os.Getenv("GEMINI_MODEL")

	// Example usage for testing a single snippet
	originalHTML := "<p>Hello <em>world</em>, this is a test.</p>"
	translated := translateNode(originalHTML, apiKey, apiUrl, model)
	fmt.Printf("Original: %s\nTranslated: %s\n", originalHTML, translated)
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
	if err != nil || resp.StatusCode != http.StatusOK {
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
