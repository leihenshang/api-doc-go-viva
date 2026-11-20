package send

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// 判断「这是一份要保存的文件，而不是一张该渲染的文本」。
//
// 两条依据，与客户端同一口径：
//  1. 服务端给了 Content-Disposition: attachment（或带 filename）——明确是下载；
//  2. Content-Type 不属于可读文本（图片 / PDF / 音视频 / 压缩包 / 二进制流）。
//
// 只看 Content-Type 不看 URL 扩展名：接口地址常以 .json 结尾但返回图片。
func (o Outcome) IsFile() bool {
	if o.Filename != "" {
		return true
	}
	return !textualType(o.ContentType)
}

// textualType 报告这个 MIME 是不是「能当文本看」：text/*、JSON、XML、表单、YAML、JS。
func textualType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return true // 没给类型：按文本渲染，用户至少能看到内容
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		media = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch {
	case strings.HasPrefix(media, "text/"):
		return true
	case strings.HasSuffix(media, "+json"), strings.HasSuffix(media, "+xml"):
		return true
	case media == "application/json", media == "application/xml", media == "application/yaml",
		media == "application/x-yaml", media == "application/javascript",
		media == "application/x-www-form-urlencoded", media == "application/xhtml+xml":
		return true
	}
	return false
}

// filenameFrom 从 Content-Disposition 里取文件名（支持 filename*= 的 UTF-8 写法）。
func filenameFrom(header http.Header) string {
	disposition := header.Get("Content-Disposition")
	if disposition == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		if name := sanitizeFilename(params["filename"]); name != "" {
			return name
		}
	}
	// 解析失败时退一手：手抠 filename= 后面的那段（有些服务端写法不规范）。
	lower := strings.ToLower(disposition)
	if index := strings.Index(lower, "filename="); index >= 0 {
		return sanitizeFilename(strings.Trim(disposition[index+len("filename="):], `"'`))
	}
	return ""
}

// sanitizeFilename 只留基名：服务端给的可能是 ../../etc/passwd 这种路径。
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}

// SuggestedFileName 是保存这份响应时的默认文件名。
func (o Outcome) SuggestedFileName() string {
	if o.Filename != "" {
		return o.Filename
	}
	extension := ".bin"
	if media, _, err := mime.ParseMediaType(o.ContentType); err == nil {
		switch {
		case media == "application/json" || strings.HasSuffix(media, "+json"):
			extension = ".json"
		case media == "application/xml" || strings.HasSuffix(media, "+xml"):
			extension = ".xml"
		case strings.HasPrefix(media, "text/"):
			extension = ".txt"
		}
	}
	return "response" + extension
}
