# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=frontend /app/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /server .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /server /server
EXPOSE 8080
ENV PORT=8080 FRONTEND_MODE=embed
ENTRYPOINT ["/server"]
