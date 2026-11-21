package privacy

import (
	"net/url"
	"strings"
)

// trackerHosts are services whose images in an email exist to report that it
// was opened. A host matches itself and its subdomains.
var trackerHosts = []string{
	"list-manage.com", "mailchimp.com", "mcusercontent.com/track", "sendgrid.net", "sendgrid.com",
	"mandrillapp.com", "mailgun.org", "mailgun.net", "mailjet.com", "sparkpostmail.com", "sparkpost.com",
	"hubspot.com", "hubspotemail.net", "hs-analytics.net", "hsforms.com", "hubspotlinks.com",
	"pardot.com", "exacttarget.com", "exct.net", "marketo.com", "mktoresp.com", "mktdns.com",
	"klaviyo.com", "klclick.com", "constantcontact.com", "rs6.net", "campaign-archive.com",
	"createsend.com", "cmail19.com", "cmail20.com", "customer.io", "intercom-mail.com", "intercom.io",
	"mixpanel.com", "google-analytics.com", "doubleclick.net", "googleadservices.com",
	"bat.bing.com", "facebook.com/tr", "pixel.wp.com", "stats.wordpress.com", "mailtrack.io",
	"getnotify.com", "yesware.com", "bananatag.com", "streak.com/api/v1/o", "mixmax.com",
	"superhuman.com", "boomeranggmail.com", "cirrusinsight.com", "salesloft.com", "outreach.io",
	"emltrk.com", "mailstat.us", "icptrack.com", "litmus.com", "emailanalyst.com", "returnpath.net",
	"convertkit-mail.com", "ck.page/open", "substack.com/o/", "beehiiv.com/o", "postmarkapp.com",
	"amazonses.com", "awstrack.me", "r.mail.", "click.e.", "open.", "track.", "tracking.",
	"links.", "email.mg.", "ct.sendgrid.net", "omnisend.com", "drip.com", "getdrip.com",
	"activehosted.com", "emarsys.net", "sailthru.com", "braze.com", "iterable.com", "cordial.io",
	"bronto.com", "responsys.net", "rsys", "zendesk.com/attachments/token", "tinyletter.com",
	"mlsend.com", "mailerlite.com", "moosend.com", "sendinblue.com", "brevo.com", "sib.",
	"aweber.com", "getresponse.com", "benchmarkemail.com", "emailoctopus.com", "buttondown.email/o",
}

// TrackerHost returns the matched tracker for a URL, or "".
func TrackerHost(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	full := host + u.Path
	low := strings.ToLower(u.Path)
	for _, t := range trackerHosts {
		switch {
		case strings.HasSuffix(t, "."): // label prefix, e.g. "track." matches track.example.com
			if strings.HasPrefix(host, t) {
				return host
			}
		case strings.Contains(t, "/"):
			if strings.HasPrefix(full, t) || strings.Contains(full, "."+t) {
				return host
			}
		case host == t || strings.HasSuffix(host, "."+t):
			return t
		case t == "rsys" && strings.Contains(host, "rsys"):
			return host
		}
	}
	// Open-tracking paths used by self-hosted senders.
	for _, p := range []string{"/open.php", "/track/open", "/wf/open", "/e/o/", "/o.gif", "/pixel.gif", "/beacon", "/trk/", "/openrate"} {
		if strings.Contains(low, p) {
			return host
		}
	}
	return ""
}
