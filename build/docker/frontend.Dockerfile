FROM oven/bun:1 AS build
WORKDIR /app
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM nginxinc/nginx-unprivileged:stable-alpine
COPY build/nginx/frontend.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
