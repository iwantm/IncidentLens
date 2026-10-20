FROM oven/bun:1 AS build
WORKDIR /app
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
ARG VITE_OTEL_EXPORTER_OTLP_ENDPOINT=
ARG VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=
ARG VITE_OTEL_SERVICE_NAME=incident-lens-web
ENV VITE_OTEL_EXPORTER_OTLP_ENDPOINT=$VITE_OTEL_EXPORTER_OTLP_ENDPOINT
ENV VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=$VITE_OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
ENV VITE_OTEL_SERVICE_NAME=$VITE_OTEL_SERVICE_NAME
RUN bun run build

FROM nginxinc/nginx-unprivileged:stable-alpine
COPY build/nginx/frontend.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
