package apiutils

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var pat = regexp.MustCompile(`^([^:@]*)(?::([^@]+))?(?:@(.*))?$`)

type ImageID struct {
	Registry string
	Repo     string
	Tag      string
	Digest   string
}

func (id ImageID) String() string {
	res := path.Join(id.Registry, id.Repo)
	if id.Tag != "" {
		res += fmt.Sprintf(":%s", id.Tag)
	}
	if id.Digest != "" {
		res += fmt.Sprintf("@%s", id.Digest)
	}
	return res
}

func (id ImageID) WithDefaults(d ImageID) ImageID {
	if id.Registry == "" {
		id.Registry = d.Registry
	}
	if id.Repo == "" {
		id.Repo = d.Repo
	}
	return id
}

func ImgSplit(s string) (res ImageID) {
	if mat := pat.FindStringSubmatch(s); len(mat) > 0 {
		res.Repo, res.Tag, res.Digest = mat[1], mat[2], mat[3]
	}
	if r := strings.SplitN(res.Repo, "/", 2); strings.Contains(r[0], ".") {
		res.Registry, res.Repo = r[0], ""
		if len(r) > 1 {
			res.Repo = r[1]
		}
	}
	return
}
