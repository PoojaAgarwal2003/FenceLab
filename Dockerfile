FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /fencelab ./cmd/fencelab \
    && mkdir /empty

FROM scratch
ARG RUN_UID=65532
ARG RUN_GID=65532
COPY --from=build /fencelab /fencelab
COPY --from=build --chown=${RUN_UID}:${RUN_GID} /empty /data
COPY THIRD_PARTY_NOTICES.md /THIRD_PARTY_NOTICES.md
COPY licenses /licenses
USER ${RUN_UID}:${RUN_GID}
ENTRYPOINT ["/fencelab"]
