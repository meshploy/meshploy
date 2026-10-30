package service

import (
	"fmt"

	meshdb "github.com/meshploy/packages/db"
)

// summary is the one line under the title: what the event is about.
func (n notice) summary() string {
	if n.Data.Detail != "" {
		return n.Data.Detail
	}
	d, _ := eventDef(n.Event)
	return d.Description
}

// buildEmail renders a notice in the email every message shares.
func buildEmail(n notice, cfg meshdb.OrgEmailConfig, to string) ([]byte, error) {
	subject := "[" + n.Brand + "] " + n.Title
	if n.Data.ServiceName != "" {
		subject += ": " + n.Data.ServiceName
	} else if n.Data.NodeName != "" {
		subject += ": " + n.Data.NodeName
	}
	footer := "Sent by " + n.Brand
	if n.Channel != "" && n.Console == "" {
		footer += fmt.Sprintf(" to the \u201c%s\u201d channel", n.Channel)
	}
	e := Email{
		Subject: subject, Heading: n.Title, Body: n.summary(), Facts: n.facts(), Error: n.Data.Error,
		Link: n.Link, Button: "View in console", Colour: n.colourHex(), Footer: footer + ".",
	}
	if n.Console != "" {
		e.Footer = "Sent by " + n.Brand + " at"
		e.FooterURL, e.FooterLink = n.ConsoleBase, n.Console
		if n.Channel != "" {
			e.FooterLink = fmt.Sprintf("%s, to the \u201c%s\u201d channel", n.Console, n.Channel)
		}
		if n.ConsoleBase != "" && n.Channel != "" {
			e.ManageURL = n.ConsoleBase + "/integrations/notifications"
		}
	}
	e.wear(n.BrandInfo)
	return e.message(cfg, to, n.At)
}
