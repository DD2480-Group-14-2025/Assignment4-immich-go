package cliflags

import (
	"slices"
	"strings"
	"testing"

	"github.com/simulot/immich-go/internal/filetypes"
)

func TestStringList_Include(t *testing.T) {
	tests := []struct {
		name string
		sl   ExtensionList
		ext  string
		want bool
	}{
		{
			name: "empty",
			sl:   ExtensionList{},
			ext:  ".jpg",
			want: true,
		},
		{
			name: ".jpg",
			sl:   ExtensionList{".jpg"},
			ext:  ".JPG",
			want: true,
		},
		{
			name: ".jpg but .heic",
			sl:   ExtensionList{".jpg"},
			ext:  ".heic",
			want: false,
		},
		{
			name: ".jpg,.mp4,.mov with .mov",
			sl:   ExtensionList{".jpg", ".mp4", ".mov"},
			ext:  ".MOV",
			want: true,
		},
		{
			name: ".jpg,.mp4,.mov with .heic",
			sl:   ExtensionList{".jpg", ".mp4", ".mov"},
			ext:  ".HEIC",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sl.Include(tt.ext); got != tt.want {
				t.Errorf("StringList.Include() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringList_Exclude(t *testing.T) {
	tests := []struct {
		name string
		sl   ExtensionList
		ext  string
		want bool
	}{
		{
			name: "empty",
			sl:   ExtensionList{},
			ext:  ".jpg",
			want: false,
		},
		{
			name: ".jpg",
			sl:   ExtensionList{".jpg"},
			ext:  ".JPG",
			want: true,
		},
		{
			name: ".jpg but .heic",
			sl:   ExtensionList{".jpg"},
			ext:  ".heic",
			want: false,
		},
		{
			name: ".jpg,.mp4,.mov with .mov",
			sl:   ExtensionList{".jpg", ".mp4", ".mov"},
			ext:  ".MOV",
			want: true,
		},
		{
			name: ".jpg,.mp4,.mov with .heic",
			sl:   ExtensionList{".jpg", ".mp4", ".mov"},
			ext:  ".HEIC",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sl.Exclude(tt.ext); got != tt.want {
				t.Errorf("StringList.Exclude() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringList_IncludeType(t *testing.T) {

	var photoTypes []string
	var videoTypes []string
	var sidecarTypes []string

	for ext, mediaType := range filetypes.DefaultSupportedMedia {
		switch mediaType {
		case filetypes.TypeImage:
			photoTypes = append(photoTypes, ext)
		case filetypes.TypeVideo:
			videoTypes = append(videoTypes, ext)
		case filetypes.TypeSidecar:
			// Sidecar should always be included in the extensions if it's main picture gets added.
			videoTypes = append(videoTypes, ext)
			photoTypes = append(photoTypes, ext)
			sidecarTypes = append(sidecarTypes, ext)
		default:
			continue
		}
	}

	tests := []struct {
		name         string
		includeType  string
		expectedExts ExtensionList
	}{
		{
			name:         "Include only images, no videos",
			includeType:  "image",
			expectedExts: ExtensionList(slices.Clone(photoTypes)),
		},
		{
			name:         "Include only videos no, photos",
			includeType:  "video",
			expectedExts: ExtensionList(slices.Clone(videoTypes)),
		},
	}

	for _, tt := range tests {
		flags := InclusionFlags{
			IncludedExtensions: ExtensionList{},
			IncludedType:       IncludeType(strings.ToUpper(tt.includeType)),
		}

		t.Run(tt.name, func(t *testing.T) {
			setIncludeTypeExtensions(&flags)

			for _, ext := range flags.IncludedExtensions {
				if len(tt.expectedExts) == 0 {
					t.Errorf("Expected was empty but still gave %v\n", ext)
				} else if !slices.Contains(tt.expectedExts, ext) {
					t.Errorf("Extension: &v missing in %v %v\n", ext, tt.expectedExts)
				}
			}
		})
	}
}
