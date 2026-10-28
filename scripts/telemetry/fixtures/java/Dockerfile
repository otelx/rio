# syntax=docker/dockerfile:1
ARG BASE_IMAGE=eclipse-temurin:21.0.12.1_1-jre-resolute@sha256:6ef3295d2cef695fd288f928a15a92081a7ce61da9e785b6fd09b2ea0c1aa8e5
FROM --platform=$BUILDPLATFORM eclipse-temurin:21.0.12.1_1-jdk-resolute@sha256:197896188467cab150da54ace1e9368e8d485f6b82b377f78cb2a2b353959bac AS build
WORKDIR /app
ADD --checksum=sha256:3e1533d0321f8815eef46750aee0111b41554f9a4644c3c4d2d404744b09f60f https://repo.maven.apache.org/maven2/ch/qos/logback/logback-classic/1.5.18/logback-classic-1.5.18.jar /app/lib/logback-classic-1.5.18.jar
ADD --checksum=sha256:85139e7b57b464f8e5e36326dd81317648bed199ccc4f98cd42585f8d7571027 https://repo.maven.apache.org/maven2/ch/qos/logback/logback-core/1.5.18/logback-core-1.5.18.jar /app/lib/logback-core-1.5.18.jar
ADD --checksum=sha256:7b751d952061954d5abfed7181c1f645d336091b679891591d63329c622eb832 https://repo.maven.apache.org/maven2/org/slf4j/slf4j-api/2.0.17/slf4j-api-2.0.17.jar /app/lib/slf4j-api-2.0.17.jar
ADD --checksum=sha256:94855942d4992f112946d3de1c334e709237b8126d8130bf07807c018a4a2120 https://repo.maven.apache.org/maven2/com/google/code/gson/gson/2.13.1/gson-2.13.1.jar /app/lib/gson-2.13.1.jar
COPY Main.java logback.xml ./
RUN javac --release 21 -cp 'lib/*' Main.java

FROM ${BASE_IMAGE}
WORKDIR /app
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["java", "-cp", "/app:/app/lib/*", "Main"]
