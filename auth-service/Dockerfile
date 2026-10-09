FROM node:22.16.0-bookworm-slim AS dependencies
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends python3 make g++ && rm -rf /var/lib/apt/lists/*
COPY package*.json ./
RUN npm ci --omit=dev
FROM node:22.16.0-bookworm-slim
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3002 MIST_AUTH_DATABASE=/data/auth.sqlite
WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY package.json server.mjs ./
USER 65532:65532
EXPOSE 3002
CMD ["node", "server.mjs"]
