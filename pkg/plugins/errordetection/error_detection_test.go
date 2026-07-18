package errordetection

// import (
// 	"context"
// 	"encoding/json"
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/require"
// 	"go.uber.org/mock/gomock"
// )

// func TestErrorDetection(t *testing.T) {
// 	tests := []struct {
// 		name           string
// 		config         *DetectErrorConfig
// 		mockServices   func(*testing.T, *abimocks.MockServices)
// 		mockContext    func(*observerstest.FilterContext)
// 		executeRequest func(*observerstest.Clock, api.HttpPluginInstance)
// 		test           func(*testing.T, *observerstest.Logger)
// 	}{
// 		{
// 			name: "rule with no conditions",
// 			// this should not match any requests
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:          "no conditions",
// 					ReportAsIssue: true,
// 				}},
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				assert.Empty(t, tl.Get("rule triggered"))
// 			},
// 		},
// 		{
// 			name: "status code matching",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:               "internal server error",
// 					TriggerStatusCodes: []string{"5xx"},
// 				}},
// 			},
// 			mockContext: func(ctx *observerstest.FilterContext) {
// 				ctx.VMetadata = map[string]any{
// 					"test": "test",
// 				}
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(map[string]string{
// 					"qpoint-request-id": "test-request",
// 				}), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(map[string]string{
// 					":status": "500",
// 				}), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				tl.Assert("matched status code", []map[string]any{{
// 					"request_status": "500",
// 					"matched_status": "5xx",
// 				}})

// 				tl.Assert("rule triggered", []map[string]any{{
// 					"rule_name":         "internal server error",
// 					"qpoint_request_id": "test-request",
// 				}})

// 				assert.Len(t, tl.Get("no artifact storage indicated"), 1)
// 			},
// 		},
// 		{
// 			name: "tag trigger",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:     "tagged requests",
// 					WithTags: []string{"test-tag:hello"},
// 				}},
// 			},
// 			mockContext: func(ctx *observerstest.FilterContext) {
// 				ctx.VTags = map[string]string{"test-tag": "hello"}
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				tl.Assert("matched tag", []map[string]any{{
// 					"tag": "test-tag:hello",
// 				}})
// 			},
// 		},
// 		{
// 			name: "multiple triggers - match",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:               "bad images",
// 					TriggerStatusCodes: []string{"5xx", "403"},
// 					OnlyCategories:     []string{"image"},
// 				}},
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(map[string]string{
// 					":status":      "403",
// 					"content-type": "image/png",
// 				}), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				tl.Assert("matched status code", []map[string]any{{
// 					"request_status": "403",
// 					"matched_status": "403",
// 				}})

// 				tl.Assert("rule triggered", []map[string]any{{
// 					"rule_name": "bad images",
// 				}})
// 			},
// 		},
// 		{
// 			name: "multiple triggers: no match",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:               "bad images",
// 					TriggerStatusCodes: []string{"5xx", "403"},
// 					OnlyCategories:     []string{"image"},
// 				}},
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(map[string]string{
// 					":status":      "403",
// 					"content-type": "text/html",
// 				}), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				assert.Empty(t, tl.Get("rule triggered"))
// 			},
// 		},
// 		{
// 			name: "duration - no match",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:            "slow requests",
// 					TriggerDuration: Duration{Duration: 1 * time.Second},
// 					ReportAsIssue:   true,
// 				}},
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				clock.Add(500 * time.Millisecond) // not slow enough
// 				resStatus := instance.ResponseHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				assert.Empty(t, tl.Get("rule triggered"))
// 			},
// 		},
// 		{
// 			name: "duration - match",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:             "slow requests",
// 					TriggerDuration:  Duration{Duration: 1 * time.Second},
// 					TriggerEmptyBody: true,
// 					ReportAsIssue:    true,
// 				}},
// 			},
// 			mockServices: func(t *testing.T, services *abimocks.MockServices) {
// 				services.EXPECT().SaveIssue(gomock.Any(), types.Issue{
// 					Timestamp: observerstest.DefaultTime.Add(1 * time.Second),
// 					Direction: "egress-external",
// 					Error:     "slow requests",
// 					Status:    422,
// 					TriggerConditions: []types.IssueTriggerCondition{
// 						{Plugin: "detect_errors", Condition: string(ConditionDuration)},
// 						{Plugin: "detect_errors", Condition: string(ConditionBody)},
// 					},
// 					TriggerReasons: []string{
// 						"duration [1s] exceeded threshold [1s]",
// 						"response body is empty",
// 					},
// 				})
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				clock.Add(1 * time.Second) // slow enough
// 				resStatus := instance.ResponseHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)
// 			},
// 			test: func(t *testing.T, tl *observerstest.Logger) {
// 				assert.Len(t, tl.Get("rule triggered"), 1)
// 			},
// 		},
// 		{
// 			name: "body and url matching",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:            "error in response",
// 					TriggerContains: "error occurred",
// 					ExcludeUrls:     []string{"https://test.com/api"},
// 					OnlyUrls:        []string{"https://test.com/test"},
// 					ReportAsIssue:   true,
// 				}},
// 			},
// 			mockContext: func(ctx *observerstest.FilterContext) {
// 				ctx.VResBody = []byte("an error occurred processing request")
// 			},
// 			mockServices: func(t *testing.T, services *abimocks.MockServices) {
// 				services.EXPECT().SaveIssue(gomock.Any(), types.Issue{
// 					Timestamp: observerstest.DefaultTime,
// 					Direction: "egress-external",
// 					Error:     "error in response",
// 					URL:       "https://test.com/test",
// 					URLPath:   "/test",
// 					Status:    422,
// 					TriggerConditions: []types.IssueTriggerCondition{
// 						{Plugin: "detect_errors", Condition: string(ConditionURL)},
// 						{Plugin: "detect_errors", Condition: string(ConditionURL)},
// 						{Plugin: "detect_errors", Condition: string(ConditionBody)},
// 					},
// 					TriggerReasons: []string{
// 						"url not in exclude list",
// 						"url in include list",
// 						"response body contains the specified content",
// 					},
// 				})
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				reqStatus := instance.RequestHeaders(observerstest.Headers(map[string]string{
// 					":path":      "/test",
// 					":authority": "test.com",
// 					":scheme":    "https",
// 				}), true)
// 				require.Equal(t, api.HeadersStatusContinue, reqStatus)

// 				resStatus := instance.ResponseHeaders(observerstest.Headers(nil), true)
// 				require.Equal(t, api.ResponseHeadersStatusContinue, resStatus)

// 				// NOTE: the frame data intentionally doesn't match the response body above since the error detection
// 				// plugin currently ignores the frame data.
// 				// frame 1
// 				resBodyStatus := instance.ResponseBody(observerstest.Buffer("frame 1"), false)
// 				require.Equal(t, api.ResponseBodyStatusStopIterationAndBuffer, resBodyStatus)
// 				// frame 2
// 				resBodyStatus = instance.ResponseBody(observerstest.Buffer("frame 2"), true)
// 				require.Equal(t, api.ResponseBodyStatusContinue, resBodyStatus)
// 			},
// 		},
// 		{
// 			name: "issue and artifact storage",
// 			config: &DetectErrorConfig{
// 				Rules: []Rule{{
// 					Name:               "tagged requests",
// 					WithTags:           []string{"test-tag:hello"},
// 					TriggerStatusCodes: []string{"123", "4xx"},
// 					ReportAsIssue:      true,
// 					RulePersists: RulePersists{
// 						RecordReqHeaders: true,
// 						RecordReqBody:    true,
// 						RecordResHeaders: true,
// 						RecordResBody:    true,
// 					},
// 				}},
// 			},
// 			mockContext: func(ctx *observerstest.FilterContext) {
// 				ctx.VTags = map[string]string{"test-tag": "hello"}
// 				ctx.VMetadata = map[string]any{
// 					"connection-id": "test-connection",
// 					"org-id":        "test-org",
// 					"endpoint-id":   "test-endpoint",
// 					"direction":     "inbound",
// 				}
// 				ctx.VReqBody = []byte("request body")
// 				ctx.VResBody = []byte("response body")
// 			},
// 			mockServices: func(t *testing.T, services *abimocks.MockServices) {
// 				services.EXPECT().SaveIssue(gomock.Any(), types.Issue{
// 					EndpointId: "test-endpoint",
// 					RequestId:  "test-request",
// 					Timestamp:  observerstest.DefaultTime,
// 					Direction:  "inbound",
// 					Error:      "tagged requests",
// 					URL:        "https://test.com/test",
// 					URLPath:    "/test",
// 					Method:     "GET",
// 					Status:     418,
// 					Tags:       []string{"test-tag:hello"},
// 					TriggerConditions: []types.IssueTriggerCondition{
// 						{Plugin: "detect_errors", Condition: string(ConditionStatus)},
// 						{Plugin: "detect_errors", Condition: string(ConditionTag)},
// 					},
// 					TriggerReasons: []string{
// 						"response status code [418] matched [4xx]",
// 						"matched tag [test-tag:hello]",
// 					},
// 				})

// 				services.EXPECT().SaveArtifact(gomock.Any(), types.Artifact{
// 					Type:        types.ArtifactType_RequestHeaders,
// 					RequestId:   "test-request",
// 					EndpointId:  "test-endpoint",
// 					Data:        []byte(`{":authority":"test.com",":method":"GET",":path":"/test",":scheme":"https","Another-Header":"another-value","Qpoint-Request-Id":"test-request","X-Header":"test-value"}`),
// 					ContentType: "application/json",
// 				})

// 				services.EXPECT().SaveArtifact(gomock.Any(), types.Artifact{
// 					Type:        types.ArtifactType_RequestBody,
// 					RequestId:   "test-request",
// 					EndpointId:  "test-endpoint",
// 					Data:        []byte("request body"),
// 					ContentType: "text/plain",
// 				})

// 				services.EXPECT().SaveArtifact(gomock.Any(), types.Artifact{
// 					Type:        types.ArtifactType_ResponseHeaders,
// 					RequestId:   "test-request",
// 					EndpointId:  "test-endpoint",
// 					Data:        []byte(`{":status":"418","Response-Header":"response-value"}`),
// 					ContentType: "application/json",
// 				})

// 				services.EXPECT().SaveArtifact(gomock.Any(), types.Artifact{
// 					Type:        types.ArtifactType_ResponseBody,
// 					RequestId:   "test-request",
// 					EndpointId:  "test-endpoint",
// 					Data:        []byte("response body"),
// 					ContentType: "text/plain",
// 				})
// 			},
// 			executeRequest: func(clock *observerstest.Clock, instance api.HttpPluginInstance) {
// 				require.Equal(
// 					t, api.HeadersStatusContinue,
// 					instance.RequestHeaders(observerstest.Headers(map[string]string{
// 						"qpoint-request-id": "test-request",
// 						"x-header":          "test-value",
// 						"another-header":    "another-value",
// 						":method":           "GET",
// 						":path":             "/test",
// 						":authority":        "test.com",
// 						":scheme":           "https",
// 					}), true),
// 				)

// 				require.Equal(
// 					t, api.RequestBodyStatusContinue,
// 					instance.RequestBody(observerstest.Buffer("request body frame"), true),
// 				)

// 				require.Equal(
// 					t, api.ResponseHeadersStatusContinue,
// 					instance.ResponseHeaders(observerstest.Headers(map[string]string{
// 						":status":         "418",
// 						"response-header": "response-value",
// 					}), true),
// 				)

// 				require.Equal(
// 					t, api.ResponseBodyStatusContinue,
// 					instance.ResponseBody(observerstest.Buffer("response body frame"), true),
// 				)
// 			},
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			ctrl := gomock.NewController(t)
// 			defer ctrl.Finish()
// 			tl := observerstest.NewLogger(t)
// 			defer tl.Sync()

// 			services := abimocks.NewMockServices(ctrl)
// 			if tt.mockServices != nil {
// 				tt.mockServices(t, services)
// 			}

// 			ctx := &observerstest.FilterContext{
// 				T:         t,
// 				VServices: services,
// 			}
// 			if tt.mockContext != nil {
// 				tt.mockContext(ctx)
// 			}

// 			filter := NewHttpFilter(tl.Logger, *observerstest.MarshalYAML(t, tt.config))
// 			instance := filter.NewInstance(ctx)
// 			clock := observerstest.NewClock(nil)
// 			instance.(*filterInstance).now = clock.Now

// 			tt.executeRequest(clock, instance)
// 			instance.Destroy()
// 			filter.Destroy()
// 			tl.Sync()
// 			if tt.test != nil {
// 				tt.test(t, tl)
// 			}
// 		})
// 	}
// }
