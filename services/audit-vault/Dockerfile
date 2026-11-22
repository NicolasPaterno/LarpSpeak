FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build
WORKDIR /src
COPY AuditVault.csproj ./
RUN dotnet restore
COPY . .
RUN dotnet publish -c Release -o /app --no-restore

FROM mcr.microsoft.com/dotnet/aspnet:10.0
WORKDIR /app
COPY --from=build /app .
EXPOSE 8080
# the runtime image has no curl/wget: probe /health with bash's /dev/tcp
HEALTHCHECK --interval=10s --timeout=3s CMD bash -c 'exec 3<>/dev/tcp/127.0.0.1/8080 && printf "GET /health HTTP/1.0\r\n\r\n" >&3 && grep -q "200 OK" <&3' || exit 1
ENTRYPOINT ["dotnet", "AuditVault.dll"]
