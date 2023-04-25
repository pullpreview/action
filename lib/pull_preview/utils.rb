module PullPreview
  module Utils
    def wait_until(max_retries = 30, interval = 5, &block)
      result = true
      retries = 0
      until block.call
        retries += 1
        if retries >= max_retries
          result = false
          break
        end
        sleep interval
      end
      result 
    end
  end
end