// Package importers 把别家的集合搬进来：Postman 导出的 JSON、另一个 OpenCollection 目录。
//
// 只做「文件落盘」这一件事：解析成 collection.RequestFile，再交给 explorer 建文件，
// 因此导入结果和手写的请求完全一样（同一套 YAML、同一个 uid 规则）。
package importers

import "github.com/leihenshang/api-doc-go-share/collection"

// Item 是一个待落盘的条目：有 Request 就是一条请求，否则是文件夹（带子项）。
type Item struct {
	Name    string
	Request *collection.RequestFile
	Items   []Item
}

// Summary 是一次导入的结果，用来回报给用户。
type Summary struct {
	Folders  int
	Requests int
	Docs     int
	// Vars 是导入来的变量（Postman 的 variable / OpenCollection 的环境），
	// 由调用方决定写成哪个环境。
	Vars []collection.EnvVar
	// Notes 是跳过或降级的东西（不支持的 body 模式、读不动的文件……），
	// 只做提示，不影响其余条目导入。
	Notes []string
}
