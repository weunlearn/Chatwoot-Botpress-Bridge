package main

import (
	"bytes"
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/translate"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	// BotURL URL for Botpress Installation
	BotURL = ""
	// BotId Bot Id from Botpress installation
	BotId = ""
	// ChatwootURL URL for Chatwoot installation
	ChatwootURL = ""
	// ChatwootBotToken Chatwoot Bot token from database
	ChatwootBotToken = os.Getenv("chatwootBotToken")
	// CustomTerminology AWS Translate Custom Terminology name
	CustomTerminology = os.Getenv("customTerminology")
	// AwsDefaultRegion AWS Lambda Runtime Region
	AwsDefaultRegion = os.Getenv("AWS_REGION")
)

type CustomAttributes struct {
	Language string `json:"language"`
}
type Sender struct {
	Name             string           `json:"name"`
	PhoneNumber      string           `json:"phone_number"`
	CustomAttributes CustomAttributes `json:"custom_attributes"`
}
type Meta struct {
	Sender Sender `json:"sender"`
}
type Conversation struct {
	Id     int    `json:"id"`
	Status string `json:"status"`
	Meta   Meta   `json:"meta"`
}
type SubmittedValues struct {
	Value string `json:"value"`
	Title string `json:"title"`
}
type Actions struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Uri     string `json:"uri"`
	Payload string `json:"payload"`
}
type Item struct {
	Title string `json:"title"`
	Value string `json:"value"`
}
type Card struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	MediaUrl    string    `json:"media_url"`
	Actions     []Actions `json:"actions"`
}

type ContentAttributes struct {
	SubmittedValue []SubmittedValues `json:"submitted_values"`
	Items          interface{}       `json:"items"`
}
type IncomingRequest struct {
	MessageType       string            `json:"message_type"`
	Content           string            `json:"content"`
	ContentType       string            `json:"content_type"`
	ContentAttributes ContentAttributes `json:"content_attributes"`
	Conversation      Conversation      `json:"conversation"`
}

type Choice struct {
	Title string `json:"title"`
	Value string `json:"value"`
}
type BotpressAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Url    string `json:"url"`
}
type Response struct {
	Type     string           `json:"type"`
	Text     string           `json:"text"`
	Choices  []Choice         `json:"choices"`
	Title    string           `json:"title"`
	Subtitle string           `json:"subtitle"`
	Image    string           `json:"image"`
	Action   []BotpressAction `json:"actions"`
}
type BotResponse struct {
	Responses []Response `json:"responses"`
}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	req := IncomingRequest{}
	log.Printf("Request: %v", request.Body)
	err := json.Unmarshal([]byte(request.Body), &req)
	if err != nil {
		log.Println("Error in Unmarshalling incoming request body\n Request body:")
		log.Println(request)
		log.Fatalln(err)
	}
	if req.MessageType == "incoming" || req.ContentAttributes.SubmittedValue != nil {
		botMessages := BotResponse{}
		botChan := make(chan *http.Response)
		var requestBody *http.Request
		if req.ContentType == "text" {
			//log.Printf("Text message: %v %T", req.Content, req.Content)
			requestBody = SendToBot(req.Conversation.Meta.Sender.Name, req.Content)
		} else if req.ContentType == "input_select" {
			//log.Printf("Button message: %v", req.ContentAttributes.SubmittedValue[0].Value)
			requestBody = SendToBot(req.Conversation.Meta.Sender.Name, req.ContentAttributes.SubmittedValue[0].Value)
		} else if req.ContentType == "cards" {
			//log.Printf("Button message: %v", req.ContentAttributes.SubmittedValue[0].Value)
			requestBody = SendToBot(req.Conversation.Meta.Sender.Name, req.ContentAttributes.SubmittedValue[0].Value)
		} else {
			//log.Printf("Placeholder text")
		}
		go SendPostAsync(requestBody, botChan)
		botResponses := <-botChan

		data, err := ioutil.ReadAll(botResponses.Body)
		if err != nil {
			log.Fatalf("ERROR!!! %v", err)
		}
		defer botResponses.Body.Close()
		err = json.Unmarshal(data, &botMessages)
		if err != nil {
			log.Printf("Error in Unmarshalling botpress response body\n Response body:")
			log.Println(request)
			log.Fatalln(err)
		}
		var chatwootResponse *http.Response

		chatwootChan := make(chan *http.Response)
		for _, response := range botMessages.Responses {
			//log.Printf("Bot responses: %v", response)
			if len(response.Text) > 0 {
				requestBody = SendToChatwoot(req.Conversation.Id, response, req.Conversation.Meta.Sender.CustomAttributes, response.Choices, response.Type)
			} else {
				requestBody = SendToChatwoot(req.Conversation.Id, response, req.Conversation.Meta.Sender.CustomAttributes, response.Choices, response.Type)

			}

			go SendPostAsync(requestBody, chatwootChan)
			chatwootResponse = <-chatwootChan
			//log.Println(chatwootResponse)
		}
		defer chatwootResponse.Body.Close()
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
		}, nil
	} else {
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
		}, nil

	}
}

func SendToBot(contact string, message string) *http.Request {
	urlString := BotURL + "/api/v1/bots/" + BotId + "/converse/" + contact

	postBody, err := json.Marshal(map[string]string{
		"text": message,
		"type": "text",
	})
	if err != nil {
		log.Fatalf("ERROR!!! %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, urlString, bytes.NewBuffer(postBody))
	req.Header.Add("Content-Type", "application/json")
	//log.Println("Send to bot built")
	return req
}

func SendToChatwoot(conversationId int, response Response, customAttributes CustomAttributes, choices []Choice, messageType string) *http.Request {
	var postBody []byte
	var err error
	//log.Printf("Target Lang: %v", customAttributes.Language)
	var items []Item
	if messageType == "single-choice" {
		for _, choice := range choices {
			//log.Println(choice.Value, choice.Title)
			items = append(items, Item{
				//Value: choice.Value,
				Value: AWSTranslate("en", customAttributes.Language, CustomTerminology, choice.Value),
				Title: AWSTranslate("en", customAttributes.Language, CustomTerminology, choice.Title),
			})
		}
		postBody, err = json.Marshal(IncomingRequest{
			MessageType: "outgoing",
			Content:     AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Text),
			ContentType: "input_select",
			ContentAttributes: ContentAttributes{
				SubmittedValue: nil,
				Items:          items,
			},
		})
	} else if messageType == "text" {
		log.Printf("Translated text: %v", AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Text))
		postBody, err = json.Marshal(IncomingRequest{
			MessageType: "outgoing",
			Content:     AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Text),
			ContentType: "text",
		})
	} else if messageType == "card" {
		var cards []Card
		var actions []Actions
		if len(response.Action) > 0 {
			actions = append(actions, Actions{
				Type: "link",
				Uri:  response.Action[0].Url,
				Text: AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Action[0].Title),
			})
		} else {
			actions = append(actions, Actions{
				Type: "postback",
			})
		}

		cards = append(cards, Card{
			Title:       AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Title),
			Description: AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Subtitle),
			MediaUrl:    response.Image,
			Actions:     actions,
		})

		postBody, err = json.Marshal(IncomingRequest{
			MessageType: "outgoing",
			Content:     AWSTranslate("en", customAttributes.Language, CustomTerminology, response.Title),
			ContentType: "cards",
			ContentAttributes: ContentAttributes{
				SubmittedValue: nil,
				Items:          cards,
			},
		})
	}
	log.Println(string(postBody))
	urlString := ChatwootURL + "/api/v1/accounts/1/conversations/" + strconv.Itoa(conversationId) + "/messages"
	req, err := http.NewRequest(http.MethodPost, urlString, bytes.NewReader(postBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("api_access_token", ChatwootBotToken)
	if err != nil {
		log.Println("Error in sending to chatwoot...")
		log.Printf("Conversation ID: %v, Messge: %v, Custom Attributes: %v\n", conversationId, response.Text, customAttributes)
		log.Fatalln(err)
	}

	return req

}

func SendPostAsync(body *http.Request, rc chan *http.Response) {
	resp, err := http.DefaultClient.Do(body)
	if err != nil {
		log.Fatalln("Oh nooooo")
	}
	rc <- resp
}

func AWSTranslate(SourceLanguage string, TargetLanguage string, TerminologyNames string, text string) string {

	//log.Printf("Translating: %v %v", text, TargetLanguage)
	var terminology []*string
	if TargetLanguage == SourceLanguage || TargetLanguage == "" {
		//log.Printf("%v %v", TargetLanguage, SourceLanguage)
		return text
	}
	terminology = append(terminology, aws.String(TerminologyNames))
	//log.Println(terminology)
	response, err := translateSession.Text(&translate.TextInput{
		SourceLanguageCode: aws.String(SourceLanguage),
		TargetLanguageCode: aws.String(TargetLanguage),
		TerminologyNames:   terminology,
		Text:               aws.String(text),
	})
	if err != nil {
		log.Fatalln(err)
	}

	return *response.TranslatedText
}

var translateSession *translate.Translate

func init() {
	translateSession = translate.New(session.Must(session.NewSession(&aws.Config{
		Region: aws.String(AwsDefaultRegion),
	})))
}

func main() {
	lambda.Start(handler)
}
