package api

import (
	"net/url"
	"strings"
	"testing"

	"gitlab.com/NebulousLabs/errors"

	"gitlab.com/NebulousLabs/Sia/modules"
)

// TestDefaultPath ensures defaultPath functions correctly.
func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name               string
		queryForm          url.Values
		subfiles           modules.SkyfileSubfiles
		defaultPath        string
		disableDefaultPath bool
		err                error
	}{
		{
			name:               "single file not multipart nil",
			queryForm:          url.Values{},
			subfiles:           nil,
			defaultPath:        "",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:               "single file not multipart empty",
			queryForm:          url.Values{modules.SkyfileDisableDefaultPathParamName: []string{"true"}},
			subfiles:           nil,
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name:               "single file not multipart set",
			queryForm:          url.Values{modules.SkyfileDefaultPathParamName: []string{"about.html"}},
			subfiles:           nil,
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},

		{
			name:               "single file multipart nil",
			queryForm:          url.Values{},
			subfiles:           modules.SkyfileSubfiles{"about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:               "single file multipart empty",
			queryForm:          url.Values{modules.SkyfileDisableDefaultPathParamName: []string{"true"}},
			subfiles:           modules.SkyfileSubfiles{"about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: true,
			err:                nil,
		},
		{
			name:               "single file multipart set to only",
			queryForm:          url.Values{modules.SkyfileDefaultPathParamName: []string{"about.html"}},
			subfiles:           modules.SkyfileSubfiles{"about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "/about.html",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:               "single file multipart set to nonexistent",
			queryForm:          url.Values{modules.SkyfileDefaultPathParamName: []string{"nonexistent.html"}},
			subfiles:           modules.SkyfileSubfiles{"about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name:               "single file multipart set to non-html",
			queryForm:          url.Values{modules.SkyfileDefaultPathParamName: []string{"about.js"}},
			subfiles:           modules.SkyfileSubfiles{"about.js": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name: "single file multipart both set",
			queryForm: url.Values{
				modules.SkyfileDefaultPathParamName:        []string{"about.html"},
				modules.SkyfileDisableDefaultPathParamName: []string{"true"},
			},
			subfiles:           modules.SkyfileSubfiles{"about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name:               "single file multipart set to non-root",
			queryForm:          url.Values{modules.SkyfileDefaultPathParamName: []string{"foo/bar/about.html"}},
			subfiles:           modules.SkyfileSubfiles{"foo/bar/about.html": modules.SkyfileSubfileMetadata{}},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},

		{
			name:      "multi file nil has index.html",
			queryForm: url.Values{},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:      "multi file nil no index.html",
			queryForm: url.Values{},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"hello.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:      "multi file set to empty",
			queryForm: url.Values{modules.SkyfileDisableDefaultPathParamName: []string{"true"}},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: true,
			err:                nil,
		},
		{
			name:      "multi file set to existing",
			queryForm: url.Values{modules.SkyfileDefaultPathParamName: []string{"about.html"}},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "/about.html",
			disableDefaultPath: false,
			err:                nil,
		},
		{
			name:      "multi file set to nonexistent",
			queryForm: url.Values{modules.SkyfileDefaultPathParamName: []string{"nonexistent.html"}},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name:      "multi file set to non-html",
			queryForm: url.Values{modules.SkyfileDefaultPathParamName: []string{"about.js"}},
			subfiles: modules.SkyfileSubfiles{
				"about.js":   modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name: "multi file both set",
			queryForm: url.Values{
				modules.SkyfileDefaultPathParamName:        []string{"about.html"},
				modules.SkyfileDisableDefaultPathParamName: []string{"true"},
			},
			subfiles: modules.SkyfileSubfiles{
				"about.html": modules.SkyfileSubfileMetadata{},
				"index.html": modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
		{
			name:      "multi file set to non-root",
			queryForm: url.Values{modules.SkyfileDefaultPathParamName: []string{"foo/bar/about.html"}},
			subfiles: modules.SkyfileSubfiles{
				"foo/bar/about.html": modules.SkyfileSubfileMetadata{},
				"foo/bar/baz.html":   modules.SkyfileSubfileMetadata{},
			},
			defaultPath:        "",
			disableDefaultPath: false,
			err:                ErrInvalidDefaultPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dp, ddp, err := defaultPath(tt.queryForm, tt.subfiles)
			if (err != nil || tt.err != nil) && !errors.Contains(err, tt.err) {
				t.Fatalf("Expected error %v, got %v\n", tt.err, err)
			}
			if dp != tt.defaultPath {
				t.Fatalf("Expected defaultPath '%v', got '%v'\n", tt.defaultPath, dp)
			}
			if ddp != tt.disableDefaultPath {
				t.Fatalf("Expected disableDefaultPath '%v', got '%v'\n", tt.disableDefaultPath, ddp)
			}
		})
	}
}

// TestParseSkylinkString is a table test for the parseSkylinkString function.
func TestParseSkylinkString(t *testing.T) {
	tests := []struct {
		name                 string
		strToParse           string
		skylink              string
		skylinkStringNoQuery string
		path                 string
		errMsg               string
	}{
		{
			name:                 "no path",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			path:                 "/",
			errMsg:               "",
		},
		{
			name:                 "no path with query",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w?foo=bar",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			path:                 "/",
			errMsg:               "",
		},
		{
			name:                 "with path to file",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar.baz",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar.baz",
			path:                 "/foo/bar.baz",
			errMsg:               "",
		},
		{
			name:                 "with path to dir with trailing slash",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar/",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar/",
			path:                 "/foo/bar/",
			errMsg:               "",
		},
		{
			name:                 "with path to dir without trailing slash",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar",
			path:                 "/foo/bar",
			errMsg:               "",
		},
		{
			name:                 "with path to file with query",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar.baz?foobar=nope",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar.baz",
			path:                 "/foo/bar.baz",
			errMsg:               "",
		},
		{
			name:                 "with path to dir with query with trailing slash",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar/?foobar=nope",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar/",
			path:                 "/foo/bar/",
			errMsg:               "",
		},
		{
			name:                 "with path to dir with query without trailing slash",
			strToParse:           "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar?foobar=nope",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo/bar",
			path:                 "/foo/bar",
			errMsg:               "",
		},
		{
			// Test URL-decoding the path within parseSkylinkString.
			name:                 "with path to dir containing both a query and an encoded '?'",
			strToParse:           "/IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo%3Fbar?foobar=nope",
			skylink:              "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w",
			skylinkStringNoQuery: "IAC6CkhNYuWZqMVr1gob1B6tPg4MrBGRzTaDvAIAeu9A9w/foo%3Fbar",
			path:                 "/foo?bar",
			errMsg:               "",
		},
		{
			name:                 "invalid skylink",
			strToParse:           "invalid_skylink/foo/bar?foobar=nope",
			skylink:              "",
			skylinkStringNoQuery: "",
			path:                 "",
			errMsg:               modules.ErrSkylinkIncorrectSize.Error(),
		},
		{
			name:                 "empty input",
			strToParse:           "",
			skylink:              "",
			skylinkStringNoQuery: "",
			path:                 "",
			errMsg:               modules.ErrSkylinkIncorrectSize.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skylink, skylinkStringNoQuery, path, err := parseSkylinkString(tt.strToParse)
			// Is there an actual or expected error?
			if err != nil || tt.errMsg != "" {
				// Actual err should contain expected err.
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("Expected error '%s', got %v\n", tt.errMsg, err)
				} else {
					// The errors match, so the test case passes.
					return
				}
			}
			if skylink.String() != tt.skylink {
				t.Fatalf("Expected skylink '%v', got '%v'\n", tt.skylink, skylink)
			}
			if skylinkStringNoQuery != tt.skylinkStringNoQuery {
				t.Fatalf("Expected skylinkStringNoQuery '%v', got '%v'\n", tt.skylinkStringNoQuery, skylinkStringNoQuery)
			}
			if path != tt.path {
				t.Fatalf("Expected path '%v', got '%v'\n", tt.path, path)
			}
		})
	}
}