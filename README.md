# Autoscaler KEDA

**[The component is ALPHA](https://github.com/knative/community/tree/main/mechanics/MATURITY-LEVELS.md)**

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/knative-extensions/autoscaler-keda)
[![Go Report Card](https://goreportcard.com/badge/knative-extensions/autoscaler-keda)](https://goreportcard.com/report/knative-extensions/autoscaler-keda)
[![Releases](https://img.shields.io/github/release-pre/knative-extensions/autoscaler-keda.svg?sort=semver)](https://github.com/knative-extensions/autoscaler-keda/releases)
[![LICENSE](https://img.shields.io/github/license/knative-extensions/autoscaler-keda.svg)](https://github.com/knative-extensions/autoscaler-keda/blob/main/LICENSE)
[![Slack Status](https://img.shields.io/badge/slack-join_chat-white.svg?logo=slack&style=social)](https://cloud-native.slack.com/archives/C04LGHDR9K7)
[![codecov](https://codecov.io/gh/knative-extensions/autoscaler-keda/branch/main/graph/badge.svg)](https://app.codecov.io/gh/knative-extensions/autoscaler-keda)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/5913/badge)](https://bestpractices.coreinfrastructure.org/projects/5913)


## Introduction

Autoscaler KEDA is an extension to the Knative Serving and a drop in replacement for the Knative Serving `autoscaler-hpa` component. It provides integration with KEDA for managing the hpa resources on behalf of the user.
The original issue is opened [here](https://github.com/knative/serving/issues/14877).

## Getting Started

Follow the details in [Installation and configuration](./DEVELOPMENT.md).
For using the component on OCP with Prometheus authentication enabled, follow the details in [OCP](./OPENSHIFT.md).

## Contributing

If you are interested in contributing, see [CONTRIBUTING.md](./CONTRIBUTING.md)
and [DEVELOPMENT.md](./DEVELOPMENT.md). For a list of all help wanted issues
across Knative, take a look at [CLOTRIBUTOR](https://clotributor.dev/search?project=knative&page=1).
