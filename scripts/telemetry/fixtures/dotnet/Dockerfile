ARG BASE_IMAGE=mcr.microsoft.com/dotnet/aspnet:8.0.33-bookworm-slim@sha256:63a1e42746a20795273c219f1577a8826644f12f67d829e818d8ccec92915493
# Keep net8.0 framework and host packs available with cleared package sources.
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:8.0.427-bookworm-slim@sha256:ff39a765f04619c8d146796fc09b38499ae5984f4a2a4c3a0a57d0022855daee AS dotnet-net8

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0.320-bookworm-slim@sha256:7e3a4bd66701a451823db2028154ae16ff191bc452596ba618ef4d0c58f9cd01 AS build
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
