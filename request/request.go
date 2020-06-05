package request

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dropbox/godropbox/errors"
	"github.com/kihahu/terraform-provider-pritunl/errortypes"
	"github.com/kihahu/terraform-provider-pritunl/schemas"
	"gopkg.in/mgo.v2/bson"
)

var client = &http.Client{
	Timeout: 2 * time.Minute,
}

type Request struct {
	Method string
	Path   string
	Query  map[string]string
	Json   interface{}
}

func (r *Request) Do(prvdr *schemas.Provider, respVal interface{}) (
	resp *http.Response, err error) {

	url := "https://" + prvdr.PritunlHost + r.Path

	authTimestamp := strconv.FormatInt(time.Now().Unix(), 10)
	authNonce := bson.NewObjectId().Hex()
	authString := strings.Join([]string{
		prvdr.PritunlToken,
		authTimestamp,
		authNonce,
		r.Method,
		r.Path,
	}, "&")

	hashFunc := hmac.New(sha256.New, []byte(prvdr.PritunlSecret))
	hashFunc.Write([]byte(authString))
	rawSignature := hashFunc.Sum(nil)
	authSig := base64.StdEncoding.EncodeToString(rawSignature)

	var body io.Reader
	if r.Json != nil {
		data, e := json.Marshal(r.Json)
		if e != nil {
			err = errortypes.RequestError{
				errors.Wrap(e, "request: Json marshal error"),
			}
			return
		}

		body = bytes.NewBuffer(data)
	}

	// Disable SSL Check for local testing
	// if prvdr.PritunlHost == "localhost" {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	// }

	req, err := http.NewRequest(r.Method, url, body)
	if err != nil {
		err = &errortypes.RequestError{
			errors.Wrap(err, "request: Failed to create request"),
		}
		return
	}

	if r.Query != nil {
		query := req.URL.Query()

		for key, val := range r.Query {
			query.Add(key, val)
		}

		req.URL.RawQuery = query.Encode()
	}

	req.Header.Set("Auth-Token", prvdr.PritunlToken)
	req.Header.Set("Auth-Timestamp", authTimestamp)
	req.Header.Set("Auth-Nonce", authNonce)
	req.Header.Set("Auth-Signature", authSig)

	if r.Json != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	log.Printf("[DEBUG] Sending Request: %s", req)

	resp, err = client.Do(req)
	if err != nil {
		err = &errortypes.RequestError{
			errors.Wrap(err, "request: Request error"),
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode == 401 {
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err = &errortypes.RequestError{
			errors.Wrapf(err, "request: Bad response status %d",
				resp.StatusCode),
		}
		return
	}

	if respVal != nil {
		info, _ := ioutil.ReadAll(resp.Body)
		err = json.Unmarshal(info, &respVal)
		log.Printf("[DEBUG] Sending Request: %s", respVal)
		if err != nil {
			err = &errortypes.ParseError{
				errors.Wrap(err, "request: Failed to parse response"),
			}
			return
		}
	}

	return
}
