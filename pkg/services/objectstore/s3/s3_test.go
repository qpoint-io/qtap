package s3

import "testing"

func Test_templateURL(t *testing.T) {
	tests := []struct {
		name     string
		template string
		values   map[string]string
		want     string
	}{
		{
			name:     "simple replacement",
			template: "https://storage.googleapis.com/{{BUCKET_NAME}}/file.txt",
			values:   map[string]string{"BUCKET_NAME": "my_gcs_bucket"},
			want:     "https://storage.googleapis.com/my_gcs_bucket/file.txt",
		},
		{
			name:     "multiple replacements",
			template: "{{SCHEME}}://{{HOST}}/{{PATH}}/{{FILE}}",
			values: map[string]string{
				"SCHEME": "https",
				"HOST":   "example.com",
				"PATH":   "data",
				"FILE":   "report.pdf",
			},
			want: "https://example.com/data/report.pdf",
		},
		{
			name:     "missing value",
			template: "https://{{HOST}}/{{PATH}}",
			values: map[string]string{
				"HOST": "example.com",
			},
			want: "https://example.com/{{PATH}}",
		},
		{
			name:     "empty template",
			template: "",
			values:   map[string]string{"KEY": "value"},
			want:     "",
		},
		{
			name:     "no placeholders",
			template: "https://example.com/static/file.txt",
			values:   map[string]string{"KEY": "value"},
			want:     "https://example.com/static/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := templateURL(tt.template, tt.values)
			if got != tt.want {
				t.Errorf("TemplateURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
