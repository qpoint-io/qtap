FROM node:20-alpine

WORKDIR /app

COPY package.json ./
RUN npm install

COPY app.js ./

CMD ["node", "app.js"]
