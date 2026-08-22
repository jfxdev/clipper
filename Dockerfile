FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend-build /src/frontend/dist ./internal/webembed/dist
RUN CGO_ENABLED=0 go build -o /clipper ./cmd/clipper

FROM alpine:3.20
RUN adduser -D -u 10001 clipper
COPY --from=backend-build /clipper /usr/local/bin/clipper
USER clipper
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/clipper"]
