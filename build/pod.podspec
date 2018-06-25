Pod::Spec.new do |spec|
  spec.name         = 'Gseke'
  spec.version      = '{{.Version}}'
  spec.license      = { :type => 'GNU Lesser General Public License, Version 3.0' }
  spec.homepage     = 'https://github.com/sekechain/go-sekechain'
  spec.authors      = { {{range .Contributors}}
		'{{.Name}}' => '{{.Email}}',{{end}}
	}
  spec.summary      = 'iOS sekechain Client'
  spec.source       = { :git => 'https://github.com/sekechain/go-sekechain.git', :commit => '{{.Commit}}' }

	spec.platform = :ios
  spec.ios.deployment_target  = '9.0'
	spec.ios.vendored_frameworks = 'Frameworks/Gseke.framework'

	spec.prepare_command = <<-CMD
    curl https://gsekestore.blob.core.windows.net/builds/{{.Archive}}.tar.gz | tar -xvz
    mkdir Frameworks
    mv {{.Archive}}/Gseke.framework Frameworks
    rm -rf {{.Archive}}
  CMD
end
