package apiutils

import (
	"reflect"
	"testing"
)

func TestImgSplit(t *testing.T) {
	for arg, imageId := range map[string]ImageID{
		"repo":                                 {Repo: "repo"},
		"repo1/repo2":                          {Repo: "repo1/repo2"},
		"repo1/repo2/repo3":                    {Repo: "repo1/repo2/repo3"},
		"registry.com":                         {Registry: "registry.com"},
		"registry.com/repo":                    {Registry: "registry.com", Repo: "repo"},
		"registry.com/doodle.noodle/repo":      {Registry: "registry.com", Repo: "doodle.noodle/repo"},
		":its-bnu":                             {Tag: "its-bnu"},
		"registry.com:its-bnu":                 {Registry: "registry.com", Tag: "its-bnu"},
		"repo1/repo2:its-bnu":                  {Repo: "repo1/repo2", Tag: "its-bnu"},
		"registry.com/repo:its-bnu":            {Registry: "registry.com", Repo: "repo", Tag: "its-bnu"},
		"@sha420:yolo12345":                    {Digest: "sha420:yolo12345"},
		"registry.com/repo@sha420:yolo12345":   {Registry: "registry.com", Repo: "repo", Digest: "sha420:yolo12345"},
		":its-bnu@sha420:yolo12345":            {Tag: "its-bnu", Digest: "sha420:yolo12345"},
		"repo:its-bnu@sha420:yolo12345":        {Repo: "repo", Tag: "its-bnu", Digest: "sha420:yolo12345"},
		"repo1/repo2:its-bnu@sha420:yolo12345": {Repo: "repo1/repo2", Tag: "its-bnu", Digest: "sha420:yolo12345"},
		"a.com/r1/r2:its-bnu@sha420:yolo12345": {Registry: "a.com", Repo: "r1/r2", Tag: "its-bnu", Digest: "sha420:yolo12345"},
	} {
		t.Run(arg, func(t *testing.T) {
			if gotRes := ImgSplit(arg); !reflect.DeepEqual(gotRes, imageId) {
				t.Errorf("ImgSplit() = %+v, want %+v", gotRes, imageId)
			}
		})
	}
}
