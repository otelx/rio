ARG BASE_IMAGE=mcr.microsoft.com/dotnet/aspnet:8.0.33-bookworm-slim@sha256:63a1e42746a20795273c219f1577a8826644f12f67d829e818d8ccec92915493
# Keep net8.0 framework and host packs available with cleared package sources.
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0.321-bookworm-slim@sha256:1dfdb27e9c2c345cb598925ccf6b13719ecfebea8467bd4745c23a3c108cdf84 AS dotnet-net8

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0.321-bookworm-slim@sha256:1dfdb27e9c2c345cb598925ccf6b13719ecfebea8467bd4745c23a3c108cdf84 AS build
COPY --from=dotnet-net8 /usr/share/dotnet/packs/ /usr/share/dotnet/packs/
WORKDIR /source
COPY Fixture.csproj NuGet.Config packages.lock.json ./
RUN dotnet restore --locked-mode
COPY Program.cs ./
RUN dotnet publish --configuration Release --no-restore --output /out -p:UseAppHost=false

FROM ${BASE_IMAGE}
WORKDIR /app
COPY --from=build /out ./
ENV ASPNETCORE_URLS=http://0.0.0.0:8080
USER $APP_UID
EXPOSE 8080
ENTRYPOINT ["dotnet", "Fixture.dll"]
