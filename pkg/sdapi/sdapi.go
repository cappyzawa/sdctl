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
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/tk3fftk/sdctl/pkg/sdctl_context"
	"gopkg.in/yaml.v2"
)

// Client wraps HTTPClient
type Client struct {
	URL        *url.URL
	HTTPClient *http.Client
}

// SDAPI has methods for control Screwdriver.cd APIs
type SDAPI struct {
	client *Client
	sdctx  sdctl_context.SdctlContext
}

type validatorResponse map[string]interface{}

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

type pipelineResponse struct {
	Name    string `json:"name"`
	SCMRepo struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
		URL    string `json:"url"`
	} `json:"scmRepo"`
}

type buildResponse struct {
	EventID int `json:"eventId"`
	JobID   int `json:"jobId"`
}

type jobResponse struct {
	PipelineID int    `json:"pipelineId"`
	Name       string `json:"name"`
}

type eventResponse struct {
	PipelineID int `json:"pipelineId"`
}

// BannerResponse represents Banner API response schema
type BannerResponse struct {
	ID         int    `json:"id"`
	Message    string `json:"message"`
	IsActive   bool   `json:"isActive"`
	CreateTime string `json:"createTime"`
	CreatedBy  string `json:"createdBy"`
	Type       string `json:"type"`
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
	case http.MethodGet:
		{
			req.Header.Add("Accept", "application/json")
		}
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		{
			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("Authorization", "Bearer "+sd.sdctx.SDJWT)
		}
	}

	return sd.client.HTTPClient.Do(req)
}

func (sd *SDAPI) GetJWT() (string, error) {
	path := "/v4/auth/token?api_token=" + sd.sdctx.UserToken
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status code should be %d, but actual is %d", http.StatusOK, res.StatusCode)
	}
	defer res.Body.Close()

	tokenResponse := new(tokenResponse)
	err = json.NewDecoder(res.Body).Decode(tokenResponse)

	return tokenResponse.JWT, err
}

func (sd *SDAPI) GetBanners() ([]BannerResponse, error) {
	path := "/v4/banners"
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return []BannerResponse{}, err
	}
	defer res.Body.Close()

	banners := new([]BannerResponse)
	err = json.NewDecoder(res.Body).Decode(banners)

	return *banners, err
}

func (sd *SDAPI) UpdateBanner(id, message, bannerType, isActive string, delete, retried bool) (BannerResponse, error) {
	path := "/v4/banners"
	method := http.MethodPost
	banner := new(BannerResponse)

	body := map[string]string{
		"type":     bannerType,
		"isActive": isActive,
	}
	if message != "" {
		body["message"] = message
	}
	if id != "" {
		method = http.MethodPut
		path = path + "/" + id
		if delete {
			method = http.MethodDelete
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return *banner, err
	}

	res, err := sd.request(context.TODO(), method, path, bytes.NewBuffer([]byte(jsonBody)))
	if err != nil {
		return *banner, err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusCreated, http.StatusOK:
		err = json.NewDecoder(res.Body).Decode(banner)
		fmt.Fprintf(os.Stdout, "Successfully %v a banner ID %v\n", method, banner.ID)
	case http.StatusNoContent:
		fmt.Fprintf(os.Stdout, "Successfully %v a banner ID %v\n", method, id)
	case http.StatusNotFound:
		err = fmt.Errorf("banner of ID %v is not found", id)
	default:
		if retried {
			err = fmt.Errorf("status code should be %d or %d, but actual is %d", http.StatusCreated, http.StatusOK, res.StatusCode)
			break
		}
		sd.sdctx.SDJWT, err = sd.GetJWT()
		if err != nil {
			return *banner, err
		}
		return sd.UpdateBanner(id, message, bannerType, isActive, delete, true)
	}

	return *banner, err
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

	res, err := sd.request(context.TODO(), http.MethodPost, path, bytes.NewBuffer([]byte(jsonBody)))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusCreated { // 201 is expected as a result of POST /events
		if retried {
			return fmt.Errorf("status code should be %d, but actual is %d", http.StatusCreated, res.StatusCode)
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

func (sd *SDAPI) Validator(yamlStr string, retried bool, output bool) error {
	path := "/v4/validator"
	body := `{"yaml":` + yamlStr + `}`

	res, err := sd.request(context.TODO(), http.MethodPost, path, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		if retried {
			return fmt.Errorf("status code should be %d, but actual is %d", http.StatusOK, res.StatusCode)
		}
		sd.sdctx.SDJWT, err = sd.GetJWT()
		if err != nil {
			return err
		}
		return sd.Validator(yamlStr, true, output)
	}
	defer res.Body.Close()

	var vr validatorResponse
	if err := json.NewDecoder(res.Body).Decode(&vr); err != nil {
		return err
	}
	if vr["errors"] != nil {
		return fmt.Errorf("%v", vr["errors"])
	}

	if output {
		if err := yaml.NewEncoder(os.Stdout).Encode(vr); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(os.Stdout, "Your screwdriver.yaml is valid🙆")
	}
	return nil
}

func (sd *SDAPI) ValidatorTemplate(yaml string, retried bool) error {
	path := "/v4/validator/template"
	body := `{"yaml":` + yaml + `}`

	res, err := sd.request(context.TODO(), http.MethodPost, path, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		if retried {
			return fmt.Errorf("status code should be %d, but actual is %d", http.StatusOK, res.StatusCode)
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
	basePipelineURL := strings.Replace(sd.sdctx.APIURL, "api-cd", "cd", 1) + "/pipelines/"

	var wg sync.WaitGroup
	wg.Add(buildIDLength)

	exit := make(chan error, buildIDLength)

	for _, b := range buildIDList {
		go func(b string) {
			defer wg.Done()

			br, err := sd.getBuild(b)
			if err != nil {
				exit <- err
				return
			}
			jr, err := sd.getJob(br.JobID)
			if err != nil {
				exit <- err
				return
			}
			pr, err := sd.getPipeline(jr.PipelineID)
			if err != nil {
				exit <- err
				return
			}
			fmt.Println(basePipelineURL + strconv.Itoa(jr.PipelineID) + "/builds/" + b + "\t" + pr.SCMRepo.Name + "(" + jr.Name + ")")
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

func (sd *SDAPI) getPipeline(pipelineID int) (*pipelineResponse, error) {
	path := "/v4/pipelines/" + strconv.Itoa(pipelineID) + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	pipelineResponse := new(pipelineResponse)
	err = json.NewDecoder(res.Body).Decode(pipelineResponse)

	return pipelineResponse, err
}

func (sd *SDAPI) getBuild(buildID string) (*buildResponse, error) {
	path := "/v4/builds/" + buildID + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	buildResponse := new(buildResponse)
	err = json.NewDecoder(res.Body).Decode(buildResponse)

	return buildResponse, err
}

func (sd *SDAPI) getJob(jobID int) (*jobResponse, error) {
	path := "/v4/jobs/" + strconv.Itoa(jobID) + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	jobResponse := new(jobResponse)
	err = json.NewDecoder(res.Body).Decode(jobResponse)

	return jobResponse, err
}

func (sd *SDAPI) getEvent(eventID int) (*eventResponse, error) {
	path := "/v4/events/" + strconv.Itoa(eventID) + "?token=" + sd.sdctx.SDJWT
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	eventResponse := new(eventResponse)
	err = json.NewDecoder(res.Body).Decode(eventResponse)

	return eventResponse, err
}

type Secret struct {
	ID         int    `json:"id"`
	PipelineID int    `json:"pipelineId"`
	Name       string `json:"name"`
	AllowInPR  bool   `json:"allowInPR"`
}

func (sd *SDAPI) SetSecret(pipelineID int, key, value string, allowInPR bool) error {

	secrets, err := sd.getPipelineSecrets(pipelineID)
	if err != nil {
		return err
	}

	duplicatedKeyID, exist := sd.checkKey(secrets, key)

	if !exist {
		return sd.createSecret(pipelineID, key, value, allowInPR)
	}

	return sd.updateSecret(duplicatedKeyID, value, allowInPR)
}

func (sd *SDAPI) getPipelineSecrets(pipelineID int) ([]Secret, error) {
	path := fmt.Sprintf("/v4/pipelines/%d/secrets?token=%s", pipelineID, sd.sdctx.SDJWT)
	res, err := sd.request(context.TODO(), http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s status code is not %d: %d", path, http.StatusOK, res.StatusCode)
	}
	var secrets []Secret
	if err := json.NewDecoder(res.Body).Decode(&secrets); err != nil {
		return nil, err
	}

	return secrets, nil
}

func (sd *SDAPI) checkKey(secrets []Secret, key string) (int, bool) {
	for _, s := range secrets {
		if s.Name == key {
			return s.ID, true
		}
	}
	return 0, false
}

func (sd *SDAPI) createSecret(pipelineID int, key, value string, allowInPR bool) error {
	path := "/v4/secrets"
	body := make(map[string]interface{})
	body["pipelineId"] = pipelineID
	body["name"] = key
	body["value"] = value
	body["allowInPR"] = allowInPR
	bodyJSON, err := json.Marshal(&body)
	if err != nil {
		return err
	}
	res, err := sd.request(context.TODO(), http.MethodPost, path, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("POST %s status code is not %d: %d", path, http.StatusCreated, res.StatusCode)
	}
	return nil
}

func (sd *SDAPI) updateSecret(secretID int, value string, allowInPR bool) error {
	path := fmt.Sprintf("/v4/secrets/%d", secretID)
	body := make(map[string]interface{})
	body["value"] = value
	body["allowInPR"] = allowInPR
	bodyJSON, err := json.Marshal(&body)
	if err != nil {
		return err
	}
	res, err := sd.request(context.TODO(), http.MethodPut, path, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT %s status code is not %d: %d", path, http.StatusOK, res.StatusCode)
	}
	return nil
}
