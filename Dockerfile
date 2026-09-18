FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN npm install -g pnpm@11.19.0 && pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:1.27-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/server .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/server ./server
COPY --from=frontend /src/frontend/dist ./web
ENV PORT=8080 DATA_FILE=/data/state.json WEB_DIR=/app/web
VOLUME /data
EXPOSE 8080
CMD ["/app/server"]
