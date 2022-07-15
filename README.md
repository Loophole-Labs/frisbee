# Frisbee

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-brightgreen.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![Tests](https://github.com/loopholelabs/frisbee/actions/workflows/tests.yml/badge.svg)](https://github.com/loopholelabs/frisbee/actions/workflows/tests.yml)
[![Benchmarks](https://github.com/loopholelabs/frisbee/actions/workflows/benchmarks.yaml/badge.svg)](https://github.com/loopholelabs/frisbee/actions/workflows/benchmarks.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/loopholelabs/frisbee)](https://goreportcard.com/report/github.com/loopholelabs/frisbee)
[![go-doc](https://godoc.org/github.com/loopholelabs/frisbee?status.svg)](https://godoc.org/github.com/loopholelabs/frisbee)

This is the [Go](http://golang.org) library for
[Frisbee](https://frpc.io/concepts/frisbee), a bring-your-own protocol messaging framework designed for performance and
stability.

[FRPC](https://frpc.io) is a lightweight, fast, and secure RPC framework for Go that uses Frisbee under the hood. This
repository houses both projects, with **FRPC** being contained in the
[protoc-gen-frpc]("/protoc-gen-frpc") folder.

**This library requires Go1.18 or later.**

## Important note about releases and stability

This repository generally follows [Semantic Versioning](https://semver.org/). However, **this library is currently in
Beta** and is still considered experimental. Breaking changes of the library will _not_ trigger a new major release. The
same is true for selected other new features explicitly marked as
**EXPERIMENTAL** in CHANGELOG.md.

## Usage and Documentation

Usage instructions and documentation for Frisbee is available
at [https://frpc.io/concepts/frisbee](https://frpc.io/concepts/frisbee). The Frisbee framework also has great
documentation coverage using [GoDoc](https://godoc.org/github.com/loopholelabs/frisbee).

## FRPC

The FRPC Generator is still in very early **Alpha**. While it is functional and being used within other products
we're building at [Loophole Labs][loophomepage], the `proto3` spec has a myriad of edge-cases that make it difficult to
guarantee validity of generated RPC frameworks without extensive real-world use.

That being said, as the library matures and usage of FRPC grows we'll be able to increase our testing
coverage and fix any edge case bugs. One of the major benefits to the RPC framework is that reading the generated code
is extremely straight forward, making it easy to debug potential issues down the line.

### Usage and Documentation

Usage instructions and documentations for FRPC are available at [https://frpc.io/](https://frpc.io).

### Unsupported Features

The Frisbee RPC Generator currently does not support the following features, though they are actively being worked on:

- `OneOf` Message Types
- Streaming Messages between the client and server

Example `Proto3` files can be found [here](/protoc-gen-frpc/examples).

## Contributing

Bug reports and pull requests are welcome on GitHub at [https://github.com/loopholelabs/frisbee][gitrepo]. For more
contribution information check
out [the contribution guide](https://github.com/loopholelabs/frisbee/blob/master/CONTRIBUTING.md).

## License

The Frisbee project is available as open source under the terms of
the [Apache License, Version 2.0](http://www.apache.org/licenses/LICENSE-2.0).

## Code of Conduct

Everyone interacting in the Frisbee project’s codebases, issue trackers, chat rooms and mailing lists is expected to follow the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).

## Project Managed By:

[![https://loopholelabs.io][loopholelabs]](https://loopholelabs.io)

[gitrepo]: https://github.com/loopholelabs/frisbee
[loopholelabs]: https://cdn.loopholelabs.io/loopholelabs/LoopholeLabsLogo.svg
[homepage]: https://loopholelabs.io/docs/frisbee
[loophomepage]: https://loopholelabs.io
