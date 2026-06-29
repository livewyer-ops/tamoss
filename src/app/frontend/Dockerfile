# Stage 1: Build
FROM node:26-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci

COPY . .

RUN npm run build

# Stage 2: Production
FROM nginx:1.31.2-alpine

# Remove default nginx config and the top-level user directive, which is only
# valid when the nginx master process starts as root.
RUN rm /etc/nginx/conf.d/default.conf \
  && sed -i '/^user /d' /etc/nginx/nginx.conf

# Copy nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy entrypoint script for runtime config injection. Strip CRLF so images
# built from Windows checkouts still start on Linux.
COPY docker-entrypoint.sh /usr/local/bin/tamoss-ui-entrypoint.sh
RUN sed -i 's/\r$//' /usr/local/bin/tamoss-ui-entrypoint.sh \
  && chmod +x /usr/local/bin/tamoss-ui-entrypoint.sh

# Copy built assets and prepare the HTML template for runtime config generation
COPY --from=build /app/dist /usr/share/nginx/html
RUN mv /usr/share/nginx/html/index.html /usr/share/nginx/html/index.html.template \
  && chown -R nginx:nginx /usr/share/nginx/html /var/cache/nginx /var/log/nginx /run

# Runtime environment variables consumed by the entrypoint.
ENV TAMOSS_API_URL=/api

USER 101:101

EXPOSE 8080

ENTRYPOINT ["/bin/sh", "/usr/local/bin/tamoss-ui-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
