package frisbeegenerator

import (
	"fmt"
	"github.com/loophole-labs/frisbee/internal/utils"
	//"github.com/loophole-labs/frisbee"
	"google.golang.org/protobuf/compiler/protogen"
	"strings"
)

var dex = 0

type generator struct {
	gen         *protogen.Plugin
	file        *protogen.File
	genFile     *protogen.GeneratedFile
	methodNames map[string]int
}

func New(gen *protogen.Plugin, file *protogen.File) *generator {
	filename := file.GeneratedFilenamePrefix + "_frisbee.pb.go"
	g := gen.NewGeneratedFile(filename, file.GoImportPath)

	return &generator{
		gen:         gen,
		file:        file,
		genFile:     g,
		methodNames: make(map[string]int),
	}
}

func (g *generator) GenerateFrisbeeFiles() {
	g.genDoNotEdit()
	g.genNeededImports()
	g.genClientInterfaces()
	g.genServerInterfaces()
	g.genMethodConsts()
	g.genClientRouterFuncs()
	g.genServerRouterFuncs()
}

func (g *generator) genDoNotEdit() {
	g.genFile.P("// Code generated by frisbeegenerator. DO NOT EDIT.")
	g.genFile.P()
	g.genFile.P("package ", g.file.GoPackageName)
	g.genFile.P()
}

func (g *generator) genNeededImports() {
	g.genFile.P("import (")
	g.genFile.P("	\"github.com/loophole-labs/frisbee\"")
	g.genFile.P(")")
}

func (g *generator) genClientInterfaces() {
	for _, service := range g.file.Services {
		g.genClientInterface(service)
	}
}

func (g *generator) genClientInterface(service *protogen.Service) {
	serviceName := utils.CamelCase(service.GoName)
	g.genFile.P("type ", serviceName, "ClientHandler interface {")
	for _, method := range service.Methods {
		g.genFile.P(getClientFuncSignature(method))
	}
	g.genFile.P("}")
}

func (g *generator) genServerInterfaces() {
	for _, service := range g.file.Services {
		g.genServerInterface(service)
	}
}

func (g *generator) genServerInterface(service *protogen.Service) {
	serviceName := utils.CamelCase(service.GoName)
	g.genFile.P("type ", serviceName, "ServerHandler interface {")
	for _, method := range service.Methods {
		g.genFile.P(getServerFuncSignature(method))
	}
	g.genFile.P("}")
}

func getClientFuncSignature(method *protogen.Method) string {
	methName := utils.CamelCase(method.GoName)
	return fmt.Sprintf("Handle%s(incomingMessage frisbee.Message, incomingContent []byte) (outgoingMessage *frisbee.Message, outgoingContent []byte, action frisbee.Action)", methName)
}

func getServerFuncSignature(method *protogen.Method) string {
	methName := utils.CamelCase(method.GoName)
	return fmt.Sprintf("Handle%s(c *frisbee.Conn, incomingMessage frisbee.Message, incomingContent []byte) (outgoingMessage *frisbee.Message, outgoingContent []byte, action frisbee.Action)", methName)
}

func (g *generator) registerMethodName(method string) {
	g.methodNames[method] = dex
	dex += 1
}

func (g *generator) genMethodConsts() {
	for _, service := range g.file.Services {
		for _, method := range service.Methods {
			g.registerMethodName(utils.CamelCase(method.GoName))
		}
	}
	kvs := make([]string, len(g.methodNames))
	for methodString, index := range g.methodNames {
		kvs[index] = fmt.Sprintf("\"%s\":%d", methodString, index+1)
	}

	g.genFile.P(fmt.Sprintf("var MessageTypes = map[string]uint16{ %s }", strings.Join(kvs, ",")))
}

func (g *generator) genClientRouterFuncs() {
	for _, service := range g.file.Services {
		g.genClientRouterFunc(service)
	}
}

func (g *generator) genClientRouterFunc(service *protogen.Service) {
	serviceName := utils.CamelCase(service.GoName)

	g.genFile.P("func init", serviceName, "ClientRouter( h ", serviceName, "ClientHandler )frisbee.ClientRouter {")
	g.genFile.P("router := make(frisbee.ClientRouter)")
	for _, method := range service.Methods {
		g.genFile.P("router[MessageTypes[\"", utils.CamelCase(method.GoName), "\"]] = h.Handle", utils.CamelCase(method.GoName))
	}
	g.genFile.P("return router")
	g.genFile.P("}")
}

func (g *generator) genServerRouterFuncs() {
	for _, service := range g.file.Services {
		g.genServerRouterFunc(service)
	}
}

func (g *generator) genServerRouterFunc(service *protogen.Service) {
	serviceName := utils.CamelCase(service.GoName)

	g.genFile.P("func init", serviceName, "ServerRouter( h ", serviceName, "ServerHandler )frisbee.ServerRouter {")
	g.genFile.P("router := make(frisbee.ServerRouter)")
	for _, method := range service.Methods {
		g.genFile.P("router[MessageTypes[\"", utils.CamelCase(method.GoName), "\"]] = h.Handle", utils.CamelCase(method.GoName))
	}
	g.genFile.P("return router")
	g.genFile.P("}")
}
