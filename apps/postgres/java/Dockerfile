FROM maven:3.9-eclipse-temurin-21 AS builder

WORKDIR /app
COPY pom.xml ./
COPY src ./src
RUN mvn package -q

FROM eclipse-temurin:21-jre-alpine

WORKDIR /app
COPY --from=builder /app/target/*.jar ./app.jar
COPY --from=builder /app/target/lib ./lib

CMD ["java", "-jar", "app.jar"]
