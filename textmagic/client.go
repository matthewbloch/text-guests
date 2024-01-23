package textmagic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	Http     *http.Client
	Base     string
	Username string
	ApiKey   string
}

type AlmostRFC3339Time struct {
	time.Time
}

type Error struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Errors  struct {
		Common []string            `json:"common,omitempty"`
		Fields map[string][]string `json:"fields,omitempty"`
	} `json:"errors"`
}

func (e Error) String() (s string) {

	s = fmt.Sprintf("%s (%d)", e.Message, e.Code)

	if e.Errors.Common != nil || e.Errors.Fields != nil {
		s += ": "
	}

	if e.Errors.Common != nil {
		s += strings.Join(e.Errors.Common, ", ")
	}

	if e.Errors.Fields != nil {
		var fields []string
		for field, errors := range e.Errors.Fields {
			fields = append(fields, fmt.Sprintf("%s: %s", field, strings.Join(errors, ", ")))
		}
		s += strings.Join(fields, ", ")
	}
	return s
}

func (e Error) Error() string { return e.String() }

type ErrorFixed string

func (e ErrorFixed) Error() string { return string(e) }

var ErrNotFound = ErrorFixed("not found")
var ErrAuth = ErrorFixed("auth failed")

func (t *AlmostRFC3339Time) UnmarshalJSON(b []byte) error {
	unquoted := strings.Trim(string(b), "\"")
	// Because TextMagic's date formats aren't RFC3339 (missing a colon)
	//   https://datatracker.ietf.org/doc/html/rfc3339#section-5.6
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700"} {
		if parsed, err := time.Parse(layout, unquoted); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("couldn't parse time %q", b)
}

func (c *Client) requestWithMap(method string, endpoint string, keys map[string]string) (*http.Request, error) {
	body, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	return c.request(method, endpoint, body)
}

func (c *Client) request(method string, endpoint string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, c.Base+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-TM-Username", c.Username)
	req.Header.Add("X-TM-Key", c.ApiKey)
	req.Header.Add("Accept", "application/json; charset=utf-8")
	req.Header.Add("Content-Type", "application/json; charset=utf-8")
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.Http.Do(req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200, 201, 202, 204:
		return resp, nil
	case 401:
		return nil, ErrAuth
	case 404:
		return nil, ErrNotFound
	}

	respErrorRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response Error
	if err := json.Unmarshal(respErrorRaw, &response); err != nil {
		return nil, err
	}

	return nil, response
}

func (c *Client) doRequest(method string, endpoint string, body []byte) (*http.Response, error) {
	req, err := c.request(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) doRequestWithMap(method string, endpoint string, keys map[string]string) (*http.Response, error) {
	req, err := c.requestWithMap(method, endpoint, keys)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func NewClient(username, apiKey string) *Client {
	return &Client{
		Http:     &http.Client{},
		Base:     "https://rest.textmagic.com",
		Username: username,
		ApiKey:   apiKey,
	}
}

func (c Client) Ping() (userId int, err error) {
	resp, err := c.doRequest("GET", "/api/v2/ping", nil)
	if err != nil {
		return 0, err
	}
	var pingResponse struct {
		UserId      int               `json:"id"`
		Ping        string            `json:"ping"`
		UtcDateTime AlmostRFC3339Time `json:"utcDateTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pingResponse); err != nil {
		return 0, err
	}
	return pingResponse.UserId, nil
}

type CustomField struct {
	Id        int               `json:"id"`
	Name      string            `json:"name"`
	CreatedAt AlmostRFC3339Time `json:"createdAt"`
}

func (c Client) GetCustomFields() (customFields []CustomField, err error) {
	resp, err := c.doRequest("GET", "/api/v2/customfields?page=1&limit=999", nil)
	if err != nil {
		return nil, err
	}
	var customFieldsResponse struct {
		Page      int           `json:"page"`
		PageCount int           `json:"pageCount"`
		Limit     int           `json:"limit"`
		Resources []CustomField `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&customFieldsResponse); err != nil {
		return nil, err
	}
	return customFieldsResponse.Resources, nil
}

type createdResponse struct {
	Id   int    `json:"id"`
	Href string `json:"href"`
}

func (c Client) CreateCustomField(name string) (field CustomField, err error) {
	resp, err := c.doRequestWithMap("POST", "/api/v2/customfields", map[string]string{"name": name})
	if err != nil {
		return CustomField{}, err
	}
	var response createdResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return CustomField{}, err
	}
	return CustomField{Id: response.Id, Name: name, CreatedAt: AlmostRFC3339Time{time.Now()}}, nil
}

func (c Client) SetCustomFieldValue(customFieldId, contactId int, value string) error {
	resp, err := c.doRequestWithMap("PUT", "/api/v2/customfields/"+fmt.Sprintf("%d", customFieldId)+"/update", map[string]string{"contactId": fmt.Sprintf("%d", contactId), "value": value})
	if err != nil {
		return err
	}
	var response createdResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return err
	}
	return nil
}

type List struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Favorited    bool   `json:"favorited"`
	MembersCount int    `json:"membersCount"`
	Service      bool   `json:"service"`
	Shared       bool   `json:"shared"`
	IsDefault    bool   `json:"isDefault"`
	Avatar       struct {
		Href string `json:"href"`
	} `json:"avatar"`
	// FIXME: And the rest
}

func (c Client) GetLists() ([]List, error) {
	resp, err := c.doRequest("GET", "/api/v2/lists?page=1&limit=999", nil)
	if err != nil {
		return nil, err
	}
	var listsResponse struct {
		Page      int    `json:"page"`
		PageCount int    `json:"pageCount"`
		Limit     int    `json:"limit"`
		Resources []List `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listsResponse); err != nil {
		return nil, err
	}
	return listsResponse.Resources, nil
}

type Country struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type CustomFieldValue struct {
	Value string `json:"value"`
	Id    int    `json:"id"`
}

type Contact struct {
	Id                int                `json:"id"`
	Favorited         bool               `json:"favorited"`
	FirstName         string             `json:"firstName"`
	LastName          string             `json:"lastName"`
	CompanyName       string             `json:"companyName"`
	Phone             string             `json:"phone"`
	Email             string             `json:"email"`
	Country           Country            `json:"country"`
	CustomFieldValues []CustomFieldValue `json:"customFields"`
	Lists             []List             `json:"lists"`
	// FIXME: and the rest https://sandbox.textmagic.com/#/Contacts/getContactByPhone
}

func (c Contact) SetCustomFieldValue(f CustomField, v string) Contact {
	for _, value := range c.CustomFieldValues {
		if value.Id == f.Id {
			value.Value = v
			return c
		}
	}
	c.CustomFieldValues = append(c.CustomFieldValues, CustomFieldValue{Value: v, Id: f.Id})
	return c
}

func (c Contact) CustomFieldValue(n int) (string, bool) {
	for _, value := range c.CustomFieldValues {
		if value.Id == n {
			return value.Value, true
		}
	}
	return "", false
}

func (c Client) GetContactByPhone(phone string) (contact Contact, err error) {
	resp, err := c.doRequestWithMap("GET", "/api/v2/contacts/phone/"+phone, map[string]string{"phone": phone})
	if err != nil {
		return Contact{}, err
	}
	var contactResponse Contact
	if err := json.NewDecoder(resp.Body).Decode(&contactResponse); err != nil {
		return Contact{}, err
	}
	return contactResponse, nil
}

func (c Client) CreateContact(contact Contact) (Contact, error) {
	type createContactRequest struct {
		FirstName         string             `json:"firstName,omitempty"`
		LastName          string             `json:"lastName,omitempty"`
		CompanyName       string             `json:"companyName,omitempty"`
		Phone             string             `json:"phone"`
		Email             string             `json:"email"`
		Favorited         bool               `json:"favorited"`
		Blocked           bool               `json:"blocked"`
		Type              int                `json:"type"`
		Lists             string             `json:"lists"`
		CustomFieldValues []CustomFieldValue `json:"customFieldValues"`
		//Local             int    `json:"local"` // FIXME: Add option to allow local number specificiation?
		//Country           string `json:"country"`
	}

	request := createContactRequest{
		FirstName:   contact.FirstName,
		LastName:    contact.LastName,
		CompanyName: contact.CompanyName,
		Phone:       contact.Phone,
		Email:       contact.Email,
		//Country:     contact.Country.Id,
		Type: -1,
	}
	for _, list := range contact.Lists {
		if request.Lists != "" {
			request.Lists = fmt.Sprintf("%s,%d", request.Lists, list.Id)
		} else {
			request.Lists = fmt.Sprintf("%d", list.Id)
		}
	}

	if len(contact.CustomFieldValues) > 0 {
		// TextMagic API seems to refuse the update with a generic "not valid" error, so until we can find
		// out what the problem is we'll fault it here instead.
		return Contact{}, fmt.Errorf("can't create custom fields from the contact, use SetCustomFieldValue instead")
	}

	body, err := json.Marshal(request)
	if err != nil {
		return Contact{}, err
	}
	resp, err := c.doRequest("POST", "/api/v2/contacts/normalized", body)
	if err != nil {
		return Contact{}, err
	}
	var response createdResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return Contact{}, err
	}
	contact.Id = response.Id
	return contact, nil
}

func (c Client) UpdateContact(contact Contact) error {
	type updateContactRequestCustomFieldValue struct {
		Id    int    `json:"id"`
		Value string `json:"value"`
	}
	type updateContactRequest struct {
		FirstName         string                                 `json:"firstName,omitempty"`
		LastName          string                                 `json:"lastName,omitempty"`
		CompanyName       string                                 `json:"companyName,omitempty"`
		Phone             string                                 `json:"phone"`
		Email             string                                 `json:"email"`
		Favorited         bool                                   `json:"favorited"`
		Blocked           bool                                   `json:"blocked"`
		Type              int                                    `json:"type"`
		Lists             string                                 `json:"lists"`
		CustomFieldValues []updateContactRequestCustomFieldValue `json:"customFieldValues,omitempty"`
		//Local             int    `json:"local"` // FIXME: Add option to allow local number specificiation?
		//Country           string `json:"country"`
	}

	if len(contact.CustomFieldValues) > 0 {
		// TextMagic API seems to refuse the update with a generic "not valid" error, so until we can find
		// out what the problem is we'll fault it here instead.
		return fmt.Errorf("can't update custom fields from the contact, use SetCustomFieldValue instead")
	}

	request := updateContactRequest{
		FirstName:   contact.FirstName,
		LastName:    contact.LastName,
		CompanyName: contact.CompanyName,
		Phone:       contact.Phone,
		Email:       contact.Email,
	}
	for _, list := range contact.Lists {
		if request.Lists != "" {
			request.Lists = fmt.Sprintf("%s,%d", request.Lists, list.Id)
		} else {
			request.Lists = fmt.Sprintf("%d", list.Id)
		}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("/api/v2/contacts/%d", contact.Id), body)
	if err != nil {
		return err
	}
	var response createdResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return err
	}
	return nil
}

type MessageToContacts struct {
	Text     string
	Contacts []Contact
	SendAt   time.Time
}

type Message struct {
	Text            string `json:"text,omitempty"`
	TemplateId      int    `json:"templateId,omitempty"`
	SendingDateTime string `json:"sendingDateTime,omitempty"`
	SendingTimeZone string `json:"sendingTimeZone,omitempty"`
	Contacts        string `json:"contacts,omitempty"`
	Lists           string `json:"lists,omitempty"`
	Phones          string `json:"phones,omitempty"`
	CutExtra        bool   `json:"cutExtra,omitempty"`
	PartsCount      int    `json:"partsCount,omitempty"`
	ReferenceId     int    `json:"referenceId,omitempty"`
	From            string `json:"from,omitempty"`
	Rrule           string `json:"rrule,omitempty"`
	CreateChat      bool   `json:"createChat,omitempty"`
	Tts             bool   `json:"tts,omitempty"`
	Local           bool   `json:"local,omitempty"`
	LocalCountry    string `json:"localCountry,omitempty"`
	Destination     string `json:"destination,omitempty"`
	Resources       string `json:"resources,omitempty"`
}

func (c Client) SendMessageToContacts(m MessageToContacts) (int, error) {
	fm := Message{Text: m.Text}
	for _, contact := range m.Contacts {
		if fm.Contacts != "" {
			fm.Contacts += ","
		}
		fm.Contacts += fmt.Sprintf("%d", contact.Id)
	}
	if m.SendAt.After(time.Now()) {
		zone, _ := m.SendAt.Zone()
		fm.SendingDateTime = m.SendAt.Format("2006-01-02 15:04:05")
		fm.SendingTimeZone = zone
	}
	messageId, _, _, scheduleId, err := c.SendMessage(fm)
	if messageId != 0 {
		return messageId, err
	} else {
		return scheduleId, err
	}
}

func (c Client) SendMessage(message Message) (messageId, sessionId, bulkId, scheduleId int, err error) {
	body, err := json.Marshal(message)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	resp, err := c.doRequest("POST", "/api/v2/messages", body)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var response struct {
		Id         int    `json:"id"`
		Href       string `json:"href"`
		Type       string `json:"type"`
		SessionId  int    `json:"sessionId"`
		BulkId     int    `json:"bulkId"`
		ScheduleId int    `json:"scheduleId"`
		MessageId  int    `json:"messageId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, 0, 0, 0, err
	}
	return response.MessageId, response.SessionId, response.BulkId, response.ScheduleId, nil
}
