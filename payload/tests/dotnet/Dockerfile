ARG RUNTIME_IMAGE=mcr.microsoft.com/dotnet/aspnet:8.0
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
WORKDIR /source
COPY . .
RUN dotnet publish --configuration Release --output /out -p:UseAppHost=false
FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY --from=build /out .
ENTRYPOINT ["dotnet", "Smoke.dll"]
