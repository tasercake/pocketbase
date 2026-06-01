package apis_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestFileToken(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "unauthorized",
			Method:          http.MethodPost,
			URL:             "/api/files/token",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "regular user",
			Method: http.MethodPost,
			URL:    "/api/files/token",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoiX3BiX3VzZXJzX2F1dGhfIiwiZXhwIjoyNTI0NjA0NDYxLCJyZWZyZXNoYWJsZSI6dHJ1ZX0.ZT3F0Z3iM-xbGgSG3LEKiEzHrPHr8t8IuHLZGGNuxLo",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
			},
			ExpectedEvents: map[string]int{
				"*":                  0,
				"OnFileTokenRequest": 1,
			},
		},
		{
			Name:   "superuser",
			Method: http.MethodPost,
			URL:    "/api/files/token",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoicGJjXzMxNDI2MzU4MjMiLCJleHAiOjI1MjQ2MDQ0NjEsInJlZnJlc2hhYmxlIjp0cnVlfQ.UXgO3j-0BumcugrFjbd7j0M4MQvbrLggLlcu_YNGjoY",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
			},
			ExpectedEvents: map[string]int{
				"*":                  0,
				"OnFileTokenRequest": 1,
			},
		},
		{
			Name:   "hook token overwrite",
			Method: http.MethodPost,
			URL:    "/api/files/token",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoicGJjXzMxNDI2MzU4MjMiLCJleHAiOjI1MjQ2MDQ0NjEsInJlZnJlc2hhYmxlIjp0cnVlfQ.UXgO3j-0BumcugrFjbd7j0M4MQvbrLggLlcu_YNGjoY",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileTokenRequest().BindFunc(func(e *core.FileTokenRequestEvent) error {
					e.Token = "test"
					return e.Next()
				})
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"test"`,
			},
			ExpectedEvents: map[string]int{
				"*":                  0,
				"OnFileTokenRequest": 1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestFileDownload(t *testing.T) {
	t.Parallel()

	_, currentFile, _, _ := runtime.Caller(0)
	dataDirRelPath := "../tests/data/"

	testFilePath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/oap640cot4yru2s/test_kfd2wYLxkz.txt")
	testImgPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png")
	testThumbCropCenterPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/70x50_300_1SEi6Q6U72.png")
	testThumbCropTopPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/70x50t_300_1SEi6Q6U72.png")
	testThumbCropBottomPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/70x50b_300_1SEi6Q6U72.png")
	testThumbFitPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/70x50f_300_1SEi6Q6U72.png")
	testThumbZeroWidthPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/0x50_300_1SEi6Q6U72.png")
	testThumbZeroHeightPath := filepath.Join(path.Dir(currentFile), dataDirRelPath, "storage/_pb_users_auth_/4q1xlclmfloku33/thumbs_300_1SEi6Q6U72.png/70x0_300_1SEi6Q6U72.png")

	testFile, fileErr := os.ReadFile(testFilePath)
	if fileErr != nil {
		t.Fatal(fileErr)
	}

	testImg, imgErr := os.ReadFile(testImgPath)
	if imgErr != nil {
		t.Fatal(imgErr)
	}

	testThumbCropCenter, thumbErr := os.ReadFile(testThumbCropCenterPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	testThumbCropTop, thumbErr := os.ReadFile(testThumbCropTopPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	testThumbCropBottom, thumbErr := os.ReadFile(testThumbCropBottomPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	testThumbFit, thumbErr := os.ReadFile(testThumbFitPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	testThumbZeroWidth, thumbErr := os.ReadFile(testThumbZeroWidthPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	testThumbZeroHeight, thumbErr := os.ReadFile(testThumbZeroHeightPath)
	if thumbErr != nil {
		t.Fatal(thumbErr)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:            "missing collection",
			Method:          http.MethodGet,
			URL:             "/api/files/missing/4q1xlclmfloku33/300_1SEi6Q6U72.png",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "missing record",
			Method:          http.MethodGet,
			URL:             "/api/files/_pb_users_auth_/missing/300_1SEi6Q6U72.png",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "missing file",
			Method:          http.MethodGet,
			URL:             "/api/files/_pb_users_auth_/4q1xlclmfloku33/missing.png",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "existing image",
			Method:          http.MethodGet,
			URL:             "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png",
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testImg)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - missing thumb (should fallback to the original)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=999x999",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError == nil {
						t.Fatal("Expected thumb error, got nil")
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testImg)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (crop center)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=70x50",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbCropCenter)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (crop top)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=70x50t",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbCropTop)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (crop bottom)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=70x50b",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbCropBottom)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (fit)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=70x50f",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbFit)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (zero width)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=0x50",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbZeroWidth)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing image - existing thumb (zero height)",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png?thumb=70x0",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError != nil {
						t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testThumbZeroHeight)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "existing non image file - thumb parameter should be ignored",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/oap640cot4yru2s/test_kfd2wYLxkz.txt?thumb=100x100",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
					if e.ThumbError == nil {
						t.Fatal("Expected thumb error, got nil")
					}
					return e.Next()
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{string(testFile)},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},

		// protected file access checks
		{
			Name:            "protected file - superuser with expired file token",
			Method:          http.MethodGet,
			URL:             "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MTY0MDk5MTY2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.nqqtqpPhxU0045F4XP_ruAkzAidYBc5oPy9ErN3XBq0",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "protected file - superuser with valid file token",
			Method:          http.MethodGet,
			URL:             "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.Lupz541xRvrktwkrl55p5pPCF77T69ZRsohsIcb2dxc",
			ExpectedStatus:  200,
			ExpectedContent: []string{"PNG"},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:    "protected file - superuser with non-whitelisted IP",
			Method:  http.MethodGet,
			URL:     "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.Lupz541xRvrktwkrl55p5pPCF77T69ZRsohsIcb2dxc",
			Headers: map[string]string{"x-test-ip": "127.0.0.1"},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().TrustedProxy = core.TrustedProxyConfig{
					Headers: []string{"x-test-ip"},
				}

				app.Settings().SuperuserIPs = []string{"0.0.0.0"}

				err := app.Save(app.Settings())
				if err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:    "protected file - superuser with whitelisted IP",
			Method:  http.MethodGet,
			URL:     "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.Lupz541xRvrktwkrl55p5pPCF77T69ZRsohsIcb2dxc",
			Headers: map[string]string{"x-test-ip": "127.0.0.1"},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().TrustedProxy = core.TrustedProxyConfig{
					Headers: []string{"x-test-ip"},
				}

				app.Settings().SuperuserIPs = []string{"127.0.0.1"}

				if err := app.Save(app.Settings()); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"PNG"},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:            "protected file - guest without view access",
			Method:          http.MethodGet,
			URL:             "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "protected file - guest with view access",
			Method: http.MethodGet,
			URL:    "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				// mock public view access
				c, err := app.FindCachedCollectionByNameOrId("demo1")
				if err != nil {
					t.Fatalf("Failed to fetch mock collection: %v", err)
				}
				c.ViewRule = types.Pointer("")
				if err := app.UnsafeWithoutHooks().Save(c); err != nil {
					t.Fatalf("Failed to update mock collection: %v", err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"PNG"},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:   "protected file - auth record without view access",
			Method: http.MethodGet,
			URL:    "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				// mock restricted user view access
				c, err := app.FindCachedCollectionByNameOrId("demo1")
				if err != nil {
					t.Fatalf("Failed to fetch mock collection: %v", err)
				}
				c.ViewRule = types.Pointer("@request.auth.verified = true")
				if err := app.UnsafeWithoutHooks().Save(c); err != nil {
					t.Fatalf("Failed to update mock collection: %v", err)
				}
			},
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "protected file - auth record with view access",
			Method: http.MethodGet,
			URL:    "/api/files/demo1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				// mock user view access
				c, err := app.FindCachedCollectionByNameOrId("demo1")
				if err != nil {
					t.Fatalf("Failed to fetch mock collection: %v", err)
				}
				c.ViewRule = types.Pointer("@request.auth.verified = false")
				if err := app.UnsafeWithoutHooks().Save(c); err != nil {
					t.Fatalf("Failed to update mock collection: %v", err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"PNG"},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},
		{
			Name:            "protected file in view (view's View API rule failure)",
			Method:          http.MethodGet,
			URL:             "/api/files/view1/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "protected file in view (view's View API rule success)",
			Method:          http.MethodGet,
			URL:             "/api/files/view1/84nmscqy84lsi1t/test_d61b33QdDU.txt?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZpbGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo",
			ExpectedStatus:  200,
			ExpectedContent: []string{"test"},
			ExpectedEvents: map[string]int{
				"*":                     0,
				"OnFileDownloadRequest": 1,
			},
		},

		// rate limit checks
		// -----------------------------------------------------------
		{
			Name:   "RateLimit rule - users:file",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 100, Label: "*:file"},
					{MaxRequests: 0, Label: "users:file"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "RateLimit rule - *:file",
			Method: http.MethodGet,
			URL:    "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 0, Label: "*:file"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		// clone for the HEAD test (the same as the original scenario but without body)
		head := scenario
		head.Method = http.MethodHead
		head.Name = ("(HEAD) " + scenario.Name)
		head.ExpectedContent = nil
		head.Test(t)

		// regular request test
		scenario.Test(t)
	}
}

func TestFileDownloadPhotosClosestThumbSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		requested string
		selected  string
	}{
		{"400x0", "400x0"},
		{"401x0", "1200x0"},
		{"800x0", "1200x0"},
		{"1800x0", "2000x0"},
		{"2400x0", "2000x0"},
	}

	for _, tt := range tests {
		t.Run(tt.requested, func(t *testing.T) {
			app, record, filename := setupThumbSelectionFileDownload(t, "photos")
			defer app.Cleanup()

			app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
				if e.ThumbError != nil {
					t.Fatalf("Expected no thumb error, got %v", e.ThumbError)
				}
				wantServedName := tt.selected + "_" + filename
				if e.ServedName != wantServedName {
					t.Fatalf("ServedName = %q, want %q", e.ServedName, wantServedName)
				}
				return e.Next()
			})

			res := performFileRequest(t, app, "/api/files/photos/"+record.Id+"/"+filename+"?thumb="+tt.requested)
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d: %s", res.StatusCode, body)
			}

			fsys, err := app.NewFilesystem()
			if err != nil {
				t.Fatal(err)
			}
			defer fsys.Close()
			selectedPath := record.BaseFilesPath() + "/thumbs_" + filename + "/" + tt.selected + "_" + filename
			if exists, _ := fsys.Exists(selectedPath); !exists {
				t.Fatalf("Expected selected thumb %q to exist", selectedPath)
			}
			if tt.requested != tt.selected {
				requestedPath := record.BaseFilesPath() + "/thumbs_" + filename + "/" + tt.requested + "_" + filename
				if exists, _ := fsys.Exists(requestedPath); exists {
					t.Fatalf("Did not expect requested unconfigured thumb %q to exist", requestedPath)
				}
			}
		})
	}
}

func TestFileDownloadNonPhotosThumbSelectionExactMatchOnly(t *testing.T) {
	t.Parallel()

	app, record, filename := setupThumbSelectionFileDownload(t, "photo_assets")
	defer app.Cleanup()

	app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
		if e.ThumbError == nil {
			t.Fatal("Expected thumb error for non-photos unconfigured size, got nil")
		}
		if e.ServedName != filename {
			t.Fatalf("ServedName = %q, want original filename %q", e.ServedName, filename)
		}
		return e.Next()
	})

	res := performFileRequest(t, app, "/api/files/photo_assets/"+record.Id+"/"+filename+"?thumb=800x0")
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected fallback status 200, got %d: %s", res.StatusCode, body)
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	for _, size := range []string{"800x0", "1200x0"} {
		thumbPath := record.BaseFilesPath() + "/thumbs_" + filename + "/" + size + "_" + filename
		if exists, _ := fsys.Exists(thumbPath); exists {
			t.Fatalf("Did not expect non-photos request to create thumb %q", thumbPath)
		}
	}
}

func setupThumbSelectionFileDownload(t *testing.T, collectionName string) (*tests.TestApp, *core.Record, string) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}

	collection := core.NewBaseCollection(collectionName)
	collection.Fields.Add(&core.FileField{
		Name:      "image",
		MaxSelect: 1,
		MaxSize:   999999,
		Thumbs:    []string{"400x0", "1200x0", "2000x0"},
	})
	if err := app.Save(collection); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}

	record := core.NewRecord(collection)
	if err := app.Save(record); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	filename := "photo.png"

	_, currentFile, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(path.Dir(currentFile), "../tests/data/storage/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png"))
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	defer fsys.Close()
	if err := fsys.Upload(data, record.BaseFilesPath()+"/"+filename); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	if _, err := app.DB().NewQuery("UPDATE " + collection.Name + " SET image={:filename} WHERE id={:id}").Bind(map[string]any{
		"filename": filename,
		"id":       record.Id,
	}).Execute(); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	record.SetRaw("image", filename)

	return app, record, filename
}

func TestFileDownloadHDRRequiredNoFallbackOrHook(t *testing.T) {
	if hdrthumb.Available() {
		t.Skip("HDR backend is available in this build")
	}
	app, record, filename := setupHDRRequiredFileDownload(t)
	defer app.Cleanup()

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()

	staleSDRPath := record.BaseFilesPath() + "/thumbs_" + filename + "/33x33_" + filename
	if err := fsys.Upload([]byte("STALE_SDR_THUMB"), staleSDRPath); err != nil {
		t.Fatal(err)
	}

	hooksCalled := 0
	app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
		hooksCalled++
		return e.String(http.StatusTeapot, "hook served")
	})

	res := performFileRequest(t, app, "/api/files/demo1/"+record.Id+"/"+filename+"?thumb=33x33")
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode < 400 || res.StatusCode >= 500 {
		t.Fatalf("Expected non-2xx client error, got %d: %s", res.StatusCode, body)
	}
	if hooksCalled != 0 {
		t.Fatalf("Expected file download hook not to run, got %d calls", hooksCalled)
	}
	if bytes.Contains(body, []byte("STALE_SDR_THUMB")) || bytes.Contains(body, []byte("hook served")) {
		t.Fatalf("Expected no stale thumb/original/hook fallback, got body %q", string(body))
	}
	if exists, _ := fsys.Exists(record.BaseFilesPath() + "/thumbs_hdr_" + filename + "/33x33_" + filename); exists {
		t.Fatal("HDR thumb should not be written when the required HDR backend is unavailable")
	}
}

func TestFileDownloadHDRRequiredViewUsesSourceField(t *testing.T) {
	if hdrthumb.Available() {
		t.Skip("HDR backend is available in this build")
	}
	app, record, filename := setupHDRRequiredFileDownload(t)
	defer app.Cleanup()

	view := new(core.Collection)
	view.Type = core.CollectionTypeView
	view.Name = "_hdr_api_view"
	view.ViewQuery = "select id, file_one from demo1"
	if err := app.Save(view); err != nil {
		t.Fatal(err)
	}

	res := performFileRequest(t, app, "/api/files/"+view.Name+"/"+record.Id+"/"+filename+"?thumb=33x33")
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode < 400 || res.StatusCode >= 500 {
		t.Fatalf("Expected view download to enforce base file field HDR policy, got %d: %s", res.StatusCode, body)
	}
}

func TestFileDownloadHDRRequiredPreservesNonHDRBehavior(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	demo1, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	fileField := demo1.Fields.GetByName("file_one").(*core.FileField)
	fileField.Protected = false
	fileField.Thumbs = []string{"33x33"}
	fileField.HdrThumbs = true
	fileField.HdrThumbsPolicy = core.FileFieldHdrThumbsPolicyRequire
	if err := app.Save(demo1); err != nil {
		t.Fatal(err)
	}

	record, err := app.FindRecordById("demo1", "al1h9ijdeojtsjy")
	if err != nil {
		t.Fatal(err)
	}
	filename := record.GetString("file_one")
	res := performFileRequest(t, app, "/api/files/demo1/"+record.Id+"/"+filename+"?thumb=33x33")
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected non-HDR source to keep existing thumbnail behavior, got %d: %s", res.StatusCode, body)
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	if exists, _ := fsys.Exists(record.BaseFilesPath() + "/thumbs_" + filename + "/33x33_" + filename); !exists {
		t.Fatal("Expected non-HDR source to use the regular thumbs_ namespace")
	}
	if exists, _ := fsys.Exists(record.BaseFilesPath() + "/thumbs_hdr_" + filename + "/33x33_" + filename); exists {
		t.Fatal("Did not expect non-HDR source to use the HDR thumbs namespace")
	}
}

func setupHDRRequiredFileDownload(t *testing.T) (*tests.TestApp, *core.Record, string) {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}

	demo1, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	fileField := demo1.Fields.GetByName("file_one").(*core.FileField)
	fileField.Protected = false
	fileField.Thumbs = []string{"33x33"}
	fileField.HdrThumbs = true
	fileField.HdrThumbsPolicy = core.FileFieldHdrThumbsPolicyRequire
	if err := app.Save(demo1); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}

	record, err := app.FindRecordById("demo1", "al1h9ijdeojtsjy")
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	filename := "current_photo_hdr.jpg"

	_, currentFile, _, _ := runtime.Caller(0)
	hdrBytes, err := os.ReadFile(filepath.Join(path.Dir(currentFile), "../tests/data/hdr/current-photo-1.jpg"))
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	defer fsys.Close()
	if err := fsys.Upload(hdrBytes, record.BaseFilesPath()+"/"+filename); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	if _, err := app.DB().NewQuery("UPDATE demo1 SET file_one={:filename} WHERE id={:id}").Bind(map[string]any{
		"filename": filename,
		"id":       record.Id,
	}).Execute(); err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	record.SetRaw("file_one", filename)

	return app, record, filename
}

func performFileRequest(t *testing.T, app core.App, url string) *http.Response {
	t.Helper()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	pbRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := pbRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	mux.ServeHTTP(recorder, req)
	return recorder.Result()
}

func TestConcurrentThumbsGeneration(t *testing.T) {
	t.Parallel()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()

	// create a dummy file field collection
	demo1, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	fileField := demo1.Fields.GetByName("file_one").(*core.FileField)
	fileField.Protected = false
	fileField.MaxSelect = 1
	fileField.MaxSize = 999999
	// new thumbs
	fileField.Thumbs = []string{"111x111", "111x222", "111x333"}
	demo1.Fields.Add(fileField)
	if err = app.Save(demo1); err != nil {
		t.Fatal(err)
	}

	fileKey := "wsmn24bux7wo113/al1h9ijdeojtsjy/300_Jsjq7RdBgA.png"

	urls := []string{
		"/api/files/" + fileKey + "?thumb=111x111",
		"/api/files/" + fileKey + "?thumb=111x111", // should still result in single thumb
		"/api/files/" + fileKey + "?thumb=111x222",
		"/api/files/" + fileKey + "?thumb=111x333",
	}

	var wg sync.WaitGroup

	wg.Add(len(urls))

	for _, url := range urls {
		go func() {
			defer wg.Done()

			recorder := httptest.NewRecorder()

			req := httptest.NewRequest("GET", url, nil)

			pbRouter, _ := apis.NewRouter(app)
			mux, _ := pbRouter.BuildMux()
			if mux != nil {
				mux.ServeHTTP(recorder, req)
			}
		}()
	}

	wg.Wait()

	// ensure that all new requested thumbs were created
	thumbKeys := []string{
		"wsmn24bux7wo113/al1h9ijdeojtsjy/thumbs_300_Jsjq7RdBgA.png/111x111_" + filepath.Base(fileKey),
		"wsmn24bux7wo113/al1h9ijdeojtsjy/thumbs_300_Jsjq7RdBgA.png/111x222_" + filepath.Base(fileKey),
		"wsmn24bux7wo113/al1h9ijdeojtsjy/thumbs_300_Jsjq7RdBgA.png/111x333_" + filepath.Base(fileKey),
	}
	for _, k := range thumbKeys {
		if exists, _ := fsys.Exists(k); !exists {
			t.Fatalf("Missing thumb %q: %v", k, err)
		}
	}
}
