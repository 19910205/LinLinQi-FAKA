FROM node:24.18.1-alpine3.24 AS builder
WORKDIR /app
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
COPY package*.json ./
RUN npm install --global --no-audit --no-fund npm@12.0.2 \
    && npm ci --no-audit --no-fund
COPY . .
RUN node -e "const value=process.env.VITE_API_BASE_URL; if (!value) throw new Error('VITE_API_BASE_URL is required'); const url=new URL(value); const local=['localhost','127.0.0.1','[::1]','::1'].includes(url.hostname); if (url.protocol !== 'https:' && !(url.protocol === 'http:' && local)) throw new Error('VITE_API_BASE_URL must use HTTPS outside local development'); if (url.username || url.password || url.search || url.hash) throw new Error('VITE_API_BASE_URL must not contain credentials, query, or fragment')" \
    && npm run build

FROM nginx:1.30.4-alpine3.24
COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=builder --chown=101:101 /app/dist /usr/share/nginx/html
USER 101:101
EXPOSE 8080
STOPSIGNAL SIGQUIT
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -T 2 -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
