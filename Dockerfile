FROM node:24-alpine AS web-build
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
ARG VITE_API_URL=
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

FROM golang:1.25-alpine AS api-build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api-build /out/api /app/api
COPY --from=web-build /web/dist /app/web
ENV WEB_ROOT=/app/web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
