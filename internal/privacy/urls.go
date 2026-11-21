package privacy

import (
	"net/url"
	"strings"
)

// trackingParams are query parameters that only identify the reader or the
// campaign. Matched case-insensitively; a trailing * matches a prefix.
var trackingParams = []string{
	"utm_*", "mc_eid", "mc_cid", "fbclid", "gclid", "gclsrc", "dclid", "gbraid", "wbraid", "msclkid",
	"yclid", "twclid", "ttclid", "li_fat_id", "igshid", "_hsenc", "_hsmi", "__hssc", "__hstc", "__hsfp",
	"hsctatracking", "mkt_tok", "vero_id", "vero_conv", "oly_anon_id", "oly_enc_id", "rb_clickid",
	"s_cid", "ml_subscriber", "ml_subscriber_hash", "trk", "trkcampaign", "sc_campaign", "sc_channel",
	"sc_content", "sc_medium", "sc_outcome", "sc_geo", "sc_country", "ns_mchannel", "ns_source",
	"ns_campaign", "ns_linkname", "ns_fee", "_branch_match_id", "_bta_tid", "_bta_c", "ck_subscriber_id",
	"spm", "si", "ref_src", "ref_url", "cmpid", "elqtrackid", "elqtrack", "elq", "elqaid", "elqat",
	"email_source", "email_campaign", "ss_source", "ss_campaign_id", "redirect_log_mongo_id",
	"redirect_mongo_id", "sb_referer_host", "mkevt", "mkcid", "mkrid", "campid", "toolid", "customid",
}

// Redirect wrappers whose real destination is a query parameter.
var redirectors = map[string]string{
	"www.google.com/url":                      "q",
	"google.com/url":                          "q",
	"l.facebook.com/l.php":                    "u",
	"lm.facebook.com/l.php":                   "u",
	"l.instagram.com/":                        "u",
	"out.reddit.com/":                         "url",
	"www.youtube.com/redirect":                "q",
	"slack-redir.net/link":                    "url",
	"t.umblr.com/redirect":                    "z",
	"exit.sc/":                                "url",
	"steamcommunity.com/linkfilter/":          "url",
	"href.li/":                                "",
	"safelinks.protection.outlook.com/":       "url",
	"nam12.safelinks.protection.outlook.com/": "url",
}

func isTrackingParam(k string) bool {
	k = strings.ToLower(k)
	for _, p := range trackingParams {
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(k, p[:len(p)-1]) {
				return true
			}
		} else if k == p {
			return true
		}
	}
	return false
}

// CleanURL unwraps known redirectors and removes tracking parameters. URLs
// it can't parse come back unchanged.
func CleanURL(raw string) string {
	for range 3 { // nested wrappers
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return raw
		}
		host := strings.ToLower(u.Host)
		key := host + u.Path
		param, ok := redirectors[key]
		if !ok && strings.HasSuffix(host, ".safelinks.protection.outlook.com") {
			param, ok = "url", true
		}
		if ok {
			if param == "" {
				if t := strings.TrimPrefix(u.RawQuery, ""); strings.HasPrefix(t, "http") {
					raw = t
					continue
				}
			} else if t := u.Query().Get(param); strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
				raw = t
				continue
			}
		}
		break
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.RawQuery != "" {
		q := u.Query()
		changed := false
		for k := range q {
			if isTrackingParam(k) {
				q.Del(k)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	if u.Fragment != "" && strings.Contains(u.Fragment, "utm_") {
		u.Fragment = ""
	}
	return u.String()
}
