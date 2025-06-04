ARG NODE_VERSION
FROM node:${NODE_VERSION}-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY *.ts .
CMD ["npm", "run", "start"]
