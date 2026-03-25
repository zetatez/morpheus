class Morpheus < Formula
  desc "Local AI agent runtime with tool execution, session persistence, MCP protocol support"
  homepage "https://github.com/zetatez/morpheus"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.intel?
      url "https://github.com/zetatez/morpheus/releases/download/v0.1.0/morph-darwin-amd64.tar.gz"
      sha256 "UPDATE_ME"
    elsif Hardware::CPU.arm?
      url "https://github.com/zetatez/morpheus/releases/download/v0.1.0/morph-darwin-arm64.tar.gz"
      sha256 "UPDATE_ME"
    end
  end

  def install
    bin.install "morpheus"
  end

  test do
    system "#{bin}/morpheus", "--version"
  end
end
