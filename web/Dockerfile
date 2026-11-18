# Build the SPA, then serve it with nginx, which also proxies /api to call-signaling (same origin: no CORS,
# SameSite=Strict refresh cookie, EventSource without headers).
FROM node:24.11.1-alpine3.22 AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.29.3-alpine
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --chmod=755 nginx/30-tls.sh /docker-entrypoint.d/30-tls.sh
COPY --from=build /src/dist /usr/share/nginx/html
