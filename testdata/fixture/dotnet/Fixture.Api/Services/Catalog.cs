namespace Fixture.Api.Services;

/// <summary>
/// Catalog lists the fixture's items.
/// </summary>
public sealed class Catalog
{
    /// <summary>ListAll returns every item.</summary>
    public IEnumerable<string> ListAll(int limit = 10) => new[] { "a", "b" }.Take(limit);

    /// <summary>Count is how many items there are.</summary>
    public int Count => 2;
}
