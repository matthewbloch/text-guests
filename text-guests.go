package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slog"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/ttacon/libphonenumber"

	"github.com/matthewbloch/text-guests/textmagic"
	"github.com/matthewbloch/text-guests/uplisting"
)

type config struct {
	TextMagicUsername string `env:"TEXTMAGIC_USERNAME,required"`
	TextMagicApiKey   string `env:"TEXTMAGIC_API_KEY,required"`
	TextMagicApiBase  string `env:"TEXTMAGIC_API_BASE" envDefault:"https://rest.textmagic.com"`

	TextMagicContactStateName string `env:"TEXTMAGIC_CONTACT_STATE_NAME,required"`
	TextMagicListName         string `env:"TEXTMAGIC_LIST_NAME,required"`

	UplistingApiKey  string `env:"UPLISTING_API_KEY,required"`
	UplistingApiBase string `env:"UPLISTING_API_BASE" envDefault:"https://connect.uplisting.io"`

	TemplateOld    string `env:"TEMPLATE_OLD,required"`
	TemplateRecent string `env:"TEMPLATE_RECENT,required"`
	TemplateDirect string `env:"TEMPLATE_DIRECT,required"`
}

func (c config) TemplateText(name string, contact textmagic.Contact) (text string) {
	switch name {
	case "OLD":
		text = c.TemplateOld
	case "RECENT":
		text = c.TemplateRecent
	case "DIRECT":
		text = c.TemplateDirect
	}
	return strings.Replace(text, "{{.FirstName}}", strings.TrimSpace(contact.FirstName), -1)
}

type contactBookingPair struct {
	contact  textmagic.Contact
	lastStay uplisting.Booking
}

type state struct {
	stateField textmagic.CustomField
	listId     int

	contacts map[string]contactBookingPair
}

func NewState() state {
	return state{
		contacts: make(map[string]contactBookingPair),
	}
}

type loggingTransport struct{}

func (s *loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	bytes, _ := httputil.DumpRequestOut(r, true)

	resp, err := http.DefaultTransport.RoundTrip(r)
	// err is returned after dumping the response

	respBytes, _ := httputil.DumpResponse(resp, true)
	bytes = append(bytes, respBytes...)

	fmt.Printf("%s\n", bytes)

	return resp, err
}

func (s state) bookingToNewContact(b uplisting.Booking) (c textmagic.Contact) {
	names := strings.SplitAfterN(b.GuestName, " ", 2)
	c.Phone = b.GuestPhone
	c.FirstName = names[0]
	c.LastName = names[1]
	c.Email = b.GuestEmail
	c.Lists = []textmagic.List{{Id: s.listId}}

	return c
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	var config config
	var state state = NewState()
	now := time.Now()
	//ctx := context.Background()

	if err := env.Parse(&config); err != nil {
		log.Fatal(err)
	}

	uplistingClient := uplisting.NewClient(config.UplistingApiKey)
	textmagicClient := textmagic.NewClient(config.TextMagicUsername, config.TextMagicApiKey)
	uplistingClient.Http = &http.Client{Transport: &loggingTransport{}}
	textmagicClient.Http = uplistingClient.Http

	if _, err := textmagicClient.Ping(); err != nil {
		slog.Error("TextMagic did not return ping:", err)
		os.Exit(1)
	}

	if fields, err := textmagicClient.GetCustomFields(); err != nil {
		slog.Error("TextMagic did not return custom fields:", "error", err)
		os.Exit(1)
	} else {
		for _, field := range fields {
			if field.Name == config.TextMagicContactStateName {
				state.stateField = field
				goto foundCustomField
			}
		}
		slog.Error("TextMagic did not have a custom field", "name", config.TextMagicContactStateName)
		os.Exit(1)
	}
foundCustomField:

	if lists, err := textmagicClient.GetLists(); err != nil {
		slog.Error("TextMagic did not return lists:", err)
		os.Exit(1)
	} else {
		for _, list := range lists {
			if list.Name == config.TextMagicListName {
				state.listId = int(list.Id)
				goto foundList
			}
		}
		slog.Error("TextMagic did not have a list", "name", config.TextMagicListName)
		os.Exit(1)
	}
foundList:

	properties, err := uplistingClient.GetProperties()
	if err != nil {
		slog.Error("Uplisting did not return list of properties:", err)
		os.Exit(1)
	}

	for _, property := range properties {
		bookings, err := uplistingClient.GetBookings(property, time.Now().Add(time.Hour*-1000), time.Now())
		if err != nil {
			slog.Error("Uplisting did not return bookings", "property", property.Name, "error", err)
		}

		for _, booking := range bookings {
			if booking.Status == "cancelled" {
				continue
			}

			/* We try to use a phone number to identify guests, but the format can be a bit
			 * loose. For now let's use libphonenumber to try to normalize it, but that feels
			 * like an intrusive default to put inside our API client.
			 */

			toParse := booking.GuestPhone
			if !strings.HasPrefix(toParse, "0") {
				toParse = "+" + toParse
			}
			normalized, err := libphonenumber.Parse(toParse, "GB")
			if err == nil {
				booking.GuestPhone = libphonenumber.Format(normalized, libphonenumber.E164)
			}

			slog.Info("Booking", "property", property.Name, "phone", booking.GuestPhone, "arrival", booking.ArrivalAt(), "departure", booking.DepartureAt(), "name", booking.GuestName)

			/* Find or create a contact */
			contact, err := textmagicClient.GetContactByPhone(booking.GuestPhone)
			if err != nil {
				if err == textmagic.ErrNotFound {
					if contact, err = textmagicClient.CreateContact(state.bookingToNewContact(booking)); err != nil {
						slog.Warn("Couldn't create contact for "+booking.GuestPhone+":", "cause", err)
						continue
					} else {
						slog.Info("Created contact for " + booking.GuestPhone)
					}
				} else {
					slog.Error("Problem fetching contact for "+booking.GuestPhone+":", "cause", err)
					continue
				}
			}

			/* For each guest (phone number), update our idea of their most recent booking */
			if pair, ok := state.contacts[booking.GuestPhone]; ok {
				if pair.lastStay.DepartureAt().Before(booking.DepartureAt()) {
					pair.lastStay = booking
				}
			} else {
				state.contacts[booking.GuestPhone] = contactBookingPair{contact, booking}
			}
		}

	}

	for phone, pair := range state.contacts {
		slog.Info("Contact", "phone", phone, "firstName", pair.contact.FirstName, "lastName", pair.contact.LastName, "lastStay", pair.lastStay.DepartureAt())
	}

	/* Now send the appropriate text for each guest */
	for _, pair := range state.contacts {
		lastStay := pair.lastStay
		contact := pair.contact

		if lastStay.DepartureAt().After(now) {
			/* Don't text people who are currently staying, or who have a booking in the future */
			continue
		}

		var lastSent time.Time
		var lastTemplate string

		// We use TextMagic to store a custom field on each contact, so we can
		// remember what we last sent them, and when. This should probably be
		// factored out somewhere.
		{
			stateRaw, ok := contact.CustomFieldValue(state.stateField.Id)
			if ok {
				parts := strings.Split(stateRaw, ",")
				if len(parts) >= 2 {
					lastTemplate = parts[0]

					timeRaw, err := strconv.Atoi(parts[1])
					if err == nil {
						lastSent = time.Unix(int64(timeRaw), 0)
					}
				}
			}
		}

		var template string
		switch lastTemplate {
		case "":
			if now.Sub(lastStay.DepartureAt()) < time.Hour*24*30 {
				/* Send them the recent template if they've stayed in the last 30 days, and we've never texted them before */
				template = "RECENT"
			} else {
				/* Send them the old template if they've stayed in the last year, and we've never texted them before */
				template = "OLD"
			}
		case "OLD":
			if lastStay.DepartureAt().After(lastSent) {
				/* Send them the recent template if we've ever sent them the old template, and they've rebooked since */
				template = "RECENT"
			}
		case "RECENT":
			if lastStay.DepartureAt().Sub(lastSent) > time.Hour*24*180 {
				/* Send them the recent template if we've sent them the recent template before, and they last booked more than 180 days ago */
				template = "RECENT"
			}
		}

		// Anyone who's booked via "uplisting" (i.e. directly) is a treasure, we have a template just for them.
		if template != "" && lastStay.Channel == "uplisting" {
			template = "DIRECT"
		}

		// Prepare the custom field value to store back to TextMagic
		newStateRaw := template + "," + strconv.Itoa(int(now.Unix()))
		// People book in the evenings, send reminders at 7pm
		sendAt := time.Date(now.Year(), now.Month(), now.Day(), 19, 0, 0, 0, time.Local)
		if sendAt.Before(now) {
			sendAt = sendAt.Add(time.Hour * 24)
		}

		message := textmagic.MessageToContacts{
			Text:     config.TemplateText(template, contact),
			Contacts: []textmagic.Contact{contact},
			SendAt:   sendAt,
		}

		if template == "" {
			continue
		}

		// hardwired test mode
		if false {
			slog.Info("Would send message to "+contact.Phone, "template", template, "sendAt", sendAt)
			slog.Info("Would update contact "+contact.Phone+":", "state", newStateRaw, "previous template", lastTemplate, "previous sendAt", lastSent)
		} else {
			if id, err := textmagicClient.SendMessageToContacts(message); err != nil {
				slog.Error("Couldn't send message to "+contact.Phone+":", "cause", err)
			} else {
				slog.Info("Sent message to "+contact.Phone, "id", id)

				// Update our state field if the message is scheduled successfully.
				if err := textmagicClient.SetCustomFieldValue(state.stateField.Id, contact.Id, newStateRaw); err != nil {
					slog.Error("Couldn't update contact "+contact.Phone+":", "cause", err)
					os.Exit(1)
				}
			}
		}
	}
}
