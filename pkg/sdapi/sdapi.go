package sdapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/tk3fftk/sdctl/pkg/sdctl_context"
)

type Client struct {
	URL        *url.URL
	HTTPClient *http.Client
}

type SDAPI struct {
	client *Client
	sdctx  sdctl_context.SdctlContext
}

type validatorResponse struct {
	Annotations   interface{} `json:"annotations"`
	Errors        []string    `json:"errors"`
	Jobs          interface{} `json:"jobs"`
	Workflow      []string    `json:"workflow"`
	WorkflowGraph interface{} `json:"workflowGraph"`
}

type templateValidatorResponse struct {
	Template interface{}             `json:"template"`
	Errors   []templateValidateError `json:"errors"`
}

type templateValidateError struct {
	Message string      `json:"message"`
	Path    []string    `json:"path"`
	Type    string      `json:"type"`
	Context interface{} `json:"context"`
}

type tokenResponse struct {
	JWT string `json:"token"`
}

type buildResponse struct {
	EventID int `json:"eventId"`
}

type eventResponse struct {
	PipelineID int `json:"pipelineId"`
}

type bannerResponse struct {
	ID int `json:"id"`
	Message string `json:"message"`
}

// New creates a SDAPI
func New(sdctx sdctl_context.SdctlContext, httpClient *http.Client) (SDAPI, error) {
	u, err := url.Parse(sdctx.APIURL)
	if err != nil {
		return SDAPI{}, err
	}

	c := &Client{
		URL:        u,
		HTTPClient: http.DefaultClient,
	}
	if httpClient != nil {
		c.HTTPClient = httpClient
	}

	s := SDAPI{
		client: c,
		sdctx:  sdctx,
	}
	return s, nil
}

func (sd *SDAPI) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url, err := sd.client.URL.Parse(path)

	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url.String(), body)
	if err != nil {
		return nil, err
	}

	switch method {
	case "GET":
		{
			req.Header.Add("Accept", "application/json")
		}
	case "POST":
		{
			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("Authorization", "Bearer "+sd.sdctx.SDJWT)
		}
	}

	res, err := sd.client.HTTPClient.Do(req)

	if err != nil {
		return nil, err
	} else if res.StatusCode/200 != 1 { // 2xx are accepted
		return res, errors.New("status code " + strconv.Itoa(res.StatusCode))
	}

	return res, nil
}

func (sd *SDAPI) GetJWT() (string, error) {
	path := "/v4/auth/token?api_token=" + sd.sdctx.UserToken
	res, err := sd.request(context.TODO(), "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	tokenResponse := new(tokenResponse)
	err = json.NewDecoder(res.Body).Decode(tokenResponse)

	return tokenResponse.JWT, err
}

func (sd *SDAPI) GetBanners() ([]bannerResponse, error) {
	path := "/v4/banners"
	res, err := sd.request(context.TODO(), "GET", path, nil)
	if err != nil {
		return []bannerResponse{}, err
	}
	defer res.Body.Close()

	banners := new([]bannerResponse)
	err = json.NewDecoder(res.Body).Decode(banners)

	return *banners, nil
}

func (sd *SDAPI) PostEvent(pipelineID string, startFrom string, retried bool) error {
	path := "/v4/events"
	body := map[string]string{
		"pipelineId": pipelineID,
		"startFrom":  startFrom,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	res, err := sd.request(context.TODO(), "POST", path, bytes.NewBuffer([]byte(jsonBody)))
	if res.StatusCode != 201 { // 201 is expected as a result of POST /events
		if retried {
			return err
		}
		sd.sdctx.SDJWT, err = sd.GetJWT()
		if err != nil {
			return err
		}
		return sd.PostEvent(pipelineID, startFrom, true)
	}
	defer res.Body.Close()

	return nil
}

func (sd *SDAPI) Validator(yaml string, retried bool) error {
	path := "/v4/validator"
	body := `{"yaml":` + yaml + `}`

	res, err := sd.request(context.TODO(), "POST", path, bytes.NewBuffer([]byte(body)))
	if res.StatusCode != 200 {
		if retried {
			return err
		}
		sd.sdctx.SDJWT, err = sd.GetJWT()
		if err != nil {
			return err
		}
		return sd.Validator(yaml, true)
	}
	defer res.Body.Close()

	validatorResponse := new(validatorResponse)
	err = json.NewDecoder(res.Body).Decode(validatorResponse)
	if err != nil {
		return err
	}
	if len(validatorResponse.Errors) != 0 {
		return errors.New(validatorResponse.Errors[0])
	}

	println("Your screwdriver.yaml is valid🙆")

	return nil
}

func (sd *SDAPI) ValidatorTemplate(yaml string, retried bool) error {
	path := "/v4/validator/template"
	body := `{"yaml":` + yaml + `}`

	res, err := sd.request(context.TODO(), "POST", path, bytes.NewBuffer([]byte(body)))
	if res.StatusCode != 200 {
		if retried {
			return err
		}
		sd.sdctx.SDJWT, err = sd.GetJWT()
		if err != nil {
			return err
		}
		return sd.ValidatorTemplate(yaml, true)
	}
	defer res.Body.Close()

	tvr := new(templateValidatorResponse)
	err = json.NewDecoder(res.Body).Decode(tvr)
	if err != nil {
		return err
	}
	if len(tvr.Errors) != 0 {
		for i := 0; i < len(tvr.Errors); i++ {
			fmt.Printf("%v\n", tvr.Errors[i].Message)
		}
		return errors.New("invalid template of Screwdriver.cd")
	}

	println("Your template is valid🙆")

	return nil
}

func (sd *SDAPI) GetPipelinePageFromBuildID(buildID string) error {
	buildIDList := strings.Split(strings.Replace(strings.TrimSpace(buildID), "\n", " ", -1), " ")
	buildIDLength := len(buildIDList)

	var wg sync.WaitGroup
	wg.Add(buildIDLength)

	exit := make(chan error, buildIDLength)

	for _, b := range buildIDList {
		go func(b string) {
			defer wg.Done()

			br, err := sd.getBuilds(b)
			if err != nil {
				exit <- err
				return
			}
			er, err := sd.getEvents(br.EventID)
			if err != nil {
				exit <- err
				return
			}
			println(strings.Replace(sd.sdctx.APIURL, "api-cd", "cd", 1) + "/pipelines/" + strconv.Itoa(er.PipelineID) + "/builds/" + b)
		}(b)
	}

	wg.Wait()

	select {
	case err := <-exit:
		return err
	default:
		return nil
	}
}

func (sd *SDAPI) getBuilds(buildID string) (*buildResponse, error) {
	path := "/v4/builds/" + buildID + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	buildResponse := new(buildResponse)
	err = json.NewDecoder(res.Body).Decode(buildResponse)

	return buildResponse, err
}

func (sd *SDAPI) getEvents(eventID int) (*eventResponse, error) {
	path := "/v4/events/" + strconv.Itoa(eventID) + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	eventResponse := new(eventResponse)
	err = json.NewDecoder(res.Body).Decode(eventResponse)

	return eventResponse, err
}
