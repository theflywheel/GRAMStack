FROM node:24-bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app/docs
COPY docs/package.json docs/package-lock.json docs/.npmrc ./
RUN npm ci
COPY docs/ ./
COPY specs/ /app/specs/
RUN npm run build

FROM node:24-bookworm-slim
ENV NODE_ENV=production PORT=8080
WORKDIR /app
COPY --from=build --chown=node:node /app/docs/public ./public
COPY --chown=node:node docs/server.mjs ./server.mjs
USER node
EXPOSE 8080
CMD ["node", "server.mjs"]
