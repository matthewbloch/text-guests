package uplisting

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	Http *http.Client
	Base string
	Key  string
}

type Property struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Nickname        string   `json:"nickname"`
	Currency        string   `json:"currency"`
	TimeZone        string   `json:"time_zone"`
	CheckInTime     int      `json:"check_in_time"`
	CheckOutTime    int      `json:"check_out_time"`
	Type            string   `json:"type"`
	MaxiumCapacity  int      `json:"maxium_capacity"`
	Bedrooms        float32  `json:"bedrooms"`
	Beds            float32  `json:"beds"`
	Bathrooms       float32  `json:"bathrooms"`
	BedTypes        []string `json:"bed_types"`
	Description     string   `json:"description"`
	CreatedAt       string   `json:"created_at"`
	UplistingDomain string   `json:"uplisting_domain"`
	PropertySlug    string   `json:"property_slug"`
}

/*
   "id": 2693371,
   "guest_name": "Rodríguez Doris",
   "preferred_guest_name": null,
   "guest_email": "rdoris.872072@guest.booking.com",
   "guest_phone": "+44 7495 044918",
   "channel": "booking_dot_com",
   "source": null,
   "note": "** THIS RESERVATION HAS BEEN PRE-PAID **BOOKING NOTE : Payment charge is GBP 8.17505This is a Smart Flex reservation.Upgraded policy: Free cancellation until 4 days before check-in.More information can be found at https://admin.booking.com/hotel/hoteladmin/extranet_ng/manage/booking.html?hotel_id=4391118&res_id=4024473571",
   "direct": false,
   "automated_messages_enabled": true,
   "automated_reviews_enabled": true,
   "booked_at": "2023-09-27T13:03:27Z",
   "manually_moved": false,
   "check_in": "2023-10-30",
   "check_out": "2023-11-04",
   "arrival_time": "16:00:00",
   "departure_time": "11:00:00",
   "number_of_nights": 5,
   "property_name": "Agar Street",
   "property_id": 7458,
   "currency": "GBP",
   "multi_unit_name": null,
   "multi_unit_id": null,
   "external_reservation_id": "4024473571",
   "number_of_guests": 2,
   "accomodation_total": 482.38,
   "cleaning_fee": 50.0,
   "extra_guest_charges": 0.0,
   "extra_charges": 0.0,
   "discounts": 0.0,
   "booking_taxes": 96.47,
   "commission": 94.33,
   "commission_vat": null,
   "other_charges": 96.47,
   "total_payout": 526.34,
   "cancellation_fee": null,
   "gross_revenue": 628.85,
   "accommodation_management_fee": null,
   "cleaning_management_fee": null,
   "total_management_fee": null,
   "payment_processing_fee": 8.18,
   "net_revenue": 429.87,
   "balance": 0.0,
   "status": "needs_check_in"
*/

type Booking struct {
	ID                         int     `json:"id"`
	Currency                   string  `json:"currency"`
	PropertyName               string  `json:"property_name"`
	PropertyID                 int     `json:"property_id"`
	CheckIn                    string  `json:"check_in"`
	CheckOut                   string  `json:"check_out"`
	ArrivalTime                string  `json:"arrival_time"`
	DepartureTime              string  `json:"departure_time"`
	NumberOfNights             int     `json:"number_of_nights"`
	GuestName                  string  `json:"guest_name"`
	GuestEmail                 string  `json:"guest_email"`
	GuestPhone                 string  `json:"guest_phone"`
	Status                     string  `json:"status"`
	Channel                    string  `json:"channel"`
	ExternalReservationID      string  `json:"external_reservation_id"`
	NumberOfGuests             int     `json:"number_of_guests"`
	AccomodationTotal          float64 `json:"accomodation_total"`
	CleaningFee                float64 `json:"cleaning_fee"`
	Commission                 float64 `json:"commission"`
	OtherCharges               float64 `json:"other_charges"`
	TotalPayout                float64 `json:"total_payout"`
	AccommodationManagementFee float64 `json:"accommodation_management_fee"`
	CleaningManagementFee      float64 `json:"cleaning_management_fee"`
	TotalManagementFee         float64 `json:"total_management_fee"`
	BookedAt                   string  `json:"booked_at"`
}

func (b Booking) ArrivalAt() time.Time {
	tm, _ := time.Parse("2006-01-02 15:04:05", b.CheckIn+" "+b.ArrivalTime)
	return tm
}

func (b Booking) DepartureAt() time.Time {
	tm, _ := time.Parse("2006-01-02 15:04:05", b.CheckOut+" "+b.DepartureTime)
	return tm
}

func (c *Client) request(endpoint string, keys map[string]string) (*http.Request, error) {
	body, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", c.Base+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.Key)))
	req.Header.Add("Accept", "application/json; charset=utf-8")
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.Http.Do(req)
	//fmt.Println(req)
	//fmt.Println(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		var respError [500]byte
		_, err := resp.Body.Read(respError[:])
		if err != nil {
			return nil, err
		}
		return nil, errors.New(string(respError[:]))
	}
	return resp, nil
}

func (c *Client) doRequest(endpoint string, keys map[string]string) (*http.Response, error) {
	req, err := c.request(endpoint, keys)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func NewClient(key string) *Client {
	return &Client{
		Http: &http.Client{},
		Base: "https://connect.uplisting.io/",
		Key:  key,
	}
}

func (c *Client) GetProperties() ([]Property, error) {
	resp, err := c.doRequest("/properties", map[string]string{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var propertiesResponse struct {
		Data []struct {
			ID         string   `json:"id"`
			Type       string   `json:"type"`
			Attributes Property `json:"attributes"`
		}
		Included any
	}

	if err := json.NewDecoder(resp.Body).Decode(&propertiesResponse); err != nil {
		return nil, err
	}

	var properties []Property
	for _, property := range propertiesResponse.Data {
		property.Attributes.ID = property.ID
		properties = append(properties, property.Attributes)
	}

	return properties, nil
}

func (c *Client) GetBookings(p Property, from time.Time, to time.Time) (bookings []Booking, err error) {
	totalPages := 1000000000
	for page := 0; page < totalPages; page++ {
		var bookingsPage []Booking
		bookingsPage, _, totalPages, err = c.GetBookingsPage(p, from, to, page)
		if err != nil {
			return nil, err
		}
		bookings = append(bookings, bookingsPage...)
	}
	fmt.Println(bookings)
	return bookings, nil
}

func (c *Client) GetBookingsPage(p Property, from time.Time, to time.Time, page int) (bookings []Booking, totalBookings int, totalPages int, e error) {
	uri := fmt.Sprintf("/bookings/%s?from=%s&to=%s&page=%d", p.ID, from.Format("2006-01-02"), to.Format("2006-01-02"), page)
	fmt.Println(uri)
	resp, err := c.doRequest(uri, map[string]string{})
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()

	var response struct {
		Bookings []Booking `json:"bookings"`
		Meta     struct {
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, 0, 0, err
	}

	return response.Bookings, response.Meta.Total, response.Meta.TotalPages, nil
}
