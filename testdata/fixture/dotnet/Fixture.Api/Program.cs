using Fixture.Api.Services;
using System.CommandLine;

var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

var group = app.MapGroup("/cs");
group.MapGet("/items", (Catalog c) => Results.Ok(c.ListAll()));
group.MapPost("/items", () => Results.Created("/cs/items/1", null));

var shards = new Option<int>("--shards", getDefaultValue: () => 2, description: "shard count");
var region = Environment.GetEnvironmentVariable("FIXTURE_REGION") ?? "eu";

app.Run();
